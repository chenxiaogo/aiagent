// Package document 文档解析与归一化。
//
// 链路（与知识库索引流程一致）：
//
//	PDF / Word / Excel / TXT / MD / HTML / CSV
//	        ↓ Parser
//	   []*schema.Document（Eino 文档，带 MetaData）
//	        ↓ Chunk
//	   Qwen Embedding → pgvector
//
// Parser 的输出统一为 Eino 的 schema.Document，好处是：
//   - 后续可直接接 Eino 的 splitter / indexer / retriever 组件；
//   - MetaData 能携带来源信息（页码、工作表、行号），检索命中后可回溯出处。
package document

import (
	"archive/zip"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"

	"aiagent/pkg/ilog"
)

// MetaData 键名。检索命中后靠这些键回溯来源。
const (
	MetaFileName   = "file_name"
	MetaFileType   = "file_type"
	MetaSource     = "source"      // 人类可读的来源，如 "报告.docx"、"Sheet1 第 3 行"
	MetaPage       = "page"        // PDF 页码 / DOCX 分段序号
	MetaSheet      = "sheet"       // Excel 工作表名
	MetaRow        = "row"         // Excel / CSV 行号
	MetaChunkIndex = "chunk_index" // 分块后由调用方填
)

// SupportedTypes 支持解析的类型（小写，不含点）。
var SupportedTypes = map[string]bool{
	"txt": true, "md": true, "html": true, "htm": true,
	"json": true, "csv": true, "code": true, "log": true,
	"pdf": true, "docx": true, "xlsx": true, "xlsm": true,
}

// Parse 按文件类型解析为 Eino 文档列表。
// 每个文档对应一个「可独立检索的片段」：PDF 按页、DOCX 按段落块、Excel 按行、纯文本整体。
func Parse(filePath, fileType, fileName string) ([]*schema.Document, error) {
	ext := strings.ToLower(strings.TrimPrefix(fileType, "."))
	if !SupportedTypes[ext] {
		return nil, fmt.Errorf("暂不支持解析的文件类型: %s", ext)
	}
	if fileName == "" {
		fileName = filepath.Base(filePath)
	}

	var (
		docs []*schema.Document
		err  error
	)
	switch ext {
	case "pdf":
		docs, err = parsePDF(filePath)
	case "docx":
		docs, err = parseDOCX(filePath)
	case "xlsx", "xlsm":
		docs, err = parseXLSX(filePath)
	case "csv":
		docs, err = parseCSV(filePath)
	case "html", "htm":
		docs, err = parseHTML(filePath)
	default:
		docs, err = parsePlain(filePath)
	}
	if err != nil {
		return nil, err
	}

	// 统一补全 MetaData，便于检索后回溯来源
	for i, d := range docs {
		if d.MetaData == nil {
			d.MetaData = map[string]any{}
		}
		d.MetaData[MetaFileName] = fileName
		d.MetaData[MetaFileType] = ext
		if _, ok := d.MetaData[MetaSource]; !ok {
			d.MetaData[MetaSource] = fileName
		}
		// 空文档直接丢掉，避免把空白向量写进库
		if strings.TrimSpace(d.Content) == "" {
			continue
		}
		_ = i
	}
	return filterEmpty(docs), nil
}

func filterEmpty(docs []*schema.Document) []*schema.Document {
	out := docs[:0]
	for _, d := range docs {
		if strings.TrimSpace(d.Content) != "" {
			out = append(out, d)
		}
	}
	return out
}

// newDoc 构造带来源信息的文档。
func newDoc(content string, source string, kv map[string]any) *schema.Document {
	meta := map[string]any{MetaSource: source}
	for k, v := range kv {
		meta[k] = v
	}
	return &schema.Document{Content: content, MetaData: meta}
}

// ---------- 各格式解析 ----------

// parsePlain 纯文本类（txt / md / json / code / log）。
func parsePlain(path string) ([]*schema.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return []*schema.Document{newDoc(string(data), filepath.Base(path), nil)}, nil
}

// parseCSV 按行解析，每行一个文档，便于命中后定位到具体行。
func parseCSV(path string) ([]*schema.Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1 // 容忍列数不一致（-1 表示不做校验）
	rows, err := reader.ReadAll()
	if err != nil {
		// 单条坏行不应让整个文件失败，退化为纯文本解析
		ilog.Warnf("parse csv %s: %v, fallback to plain text", filepath.Base(path), err)
		return parsePlain(path)
	}

	base := filepath.Base(path)
	docs := make([]*schema.Document, 0, len(rows))
	for i, row := range rows {
		line := strings.TrimSpace(strings.Join(row, " | "))
		if line == "" {
			continue
		}
		docs = append(docs, newDoc(line, fmt.Sprintf("%s 第 %d 行", base, i+1),
			map[string]any{MetaRow: i + 1}))
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("CSV 文件无有效内容")
	}
	return docs, nil
}

// parseHTML 去掉脚本/样式标签后按纯文本处理。
func parseHTML(path string) ([]*schema.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := stripHTML(string(data))
	return []*schema.Document{newDoc(text, filepath.Base(path), nil)}, nil
}

// stripHTML 移除 script/style 块与所有标签，压缩空白。
func stripHTML(s string) string {
	// 先删掉整段 script / style
	re := strings.NewReplacer()
	_ = re
	for _, block := range []string{"script", "style"} {
		for {
			start := indexFold(s, "<"+block)
			if start < 0 {
				break
			}
			end := indexFold(s[start:], "</"+block+">")
			if end < 0 {
				s = s[:start]
				break
			}
			s = s[:start] + s[start+end+len("</"+block+">"):]
		}
	}
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteRune(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
}

func indexFold(s, sub string) int {
	return strings.Index(strings.ToLower(s), strings.ToLower(sub))
}

// parsePDF 按页提取文本，每页一个文档。
func parsePDF(path string) ([]*schema.Document, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 PDF 失败: %w", err)
	}
	defer f.Close()

	base := filepath.Base(path)
	total := r.NumPage()
	if total <= 0 {
		return nil, fmt.Errorf("PDF 无有效页面")
	}

	docs := make([]*schema.Document, 0, total)
	for i := 1; i <= total; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		// 按文本块读取；加密或异常页会返回错误，跳过该页而不是让整个文件失败
		raw, err := page.GetPlainText(nil)
		if err != nil {
			ilog.Warnf("parse pdf %s page %d: %v", base, i, err)
			continue
		}
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		docs = append(docs, newDoc(text, fmt.Sprintf("%s 第 %d 页", base, i),
			map[string]any{MetaPage: i}))
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("PDF 未提取到文本（可能是扫描件或加密文档）")
	}
	return docs, nil
}

// parseDOCX 用标准库解析：docx 本质是 zip，正文在 word/document.xml。
// 不引入 unioffice 之类的库，避免 AGPL 授权问题。
func parseDOCX(path string) ([]*schema.Document, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("打开 DOCX 失败: %w", err)
	}
	defer r.Close()

	var docFile *zip.File
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}
	if docFile == nil {
		return nil, fmt.Errorf("DOCX 缺少 word/document.xml")
	}
	rc, err := docFile.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	paras, err := extractDocxParagraphs(rc)
	if err != nil {
		return nil, err
	}

	base := filepath.Base(path)
	// 按段落聚合成块：避免每个段落一个文档导致碎片化，
	// 每攒够约 1500 字符或遇到空段落就切一块。
	docs := make([]*schema.Document, 0, 8)
	var b strings.Builder
	idx := 0
	flush := func() {
		text := strings.TrimSpace(b.String())
		b.Reset()
		if text == "" {
			return
		}
		idx++
		docs = append(docs, newDoc(text, fmt.Sprintf("%s 第 %d 段", base, idx),
			map[string]any{MetaPage: idx}))
	}
	for _, p := range paras {
		if strings.TrimSpace(p) == "" {
			flush()
			continue
		}
		b.WriteString(p)
		b.WriteString("\n")
		if b.Len() >= 1500 {
			flush()
		}
	}
	flush()

	if len(docs) == 0 {
		return nil, fmt.Errorf("DOCX 未提取到文本")
	}
	return docs, nil
}

// extractDocxParagraphs 从 document.xml 提取段落文本（w:p → 内部所有 w:t）。
func extractDocxParagraphs(r io.Reader) ([]string, error) {
	dec := xml.NewDecoder(r)
	var (
		paras   []string
		builder strings.Builder
		inText  bool
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析 document.xml 失败: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p": // 段落开始
				builder.Reset()
			case "tab":
				builder.WriteString("\t")
			case "br":
				builder.WriteString("\n")
			case "t":
				inText = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p": // 段落结束
				paras = append(paras, strings.TrimSpace(builder.String()))
				builder.Reset()
			}
		case xml.CharData:
			if inText {
				builder.Write(t)
			}
		}
	}
	return paras, nil
}

// parseXLSX 逐工作表逐行解析，每行一个文档。
func parseXLSX(path string) ([]*schema.Document, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("打开 Excel 失败: %w", err)
	}
	defer f.Close()

	base := filepath.Base(path)
	docs := make([]*schema.Document, 0, 64)
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			ilog.Warnf("read sheet %s of %s: %v", sheet, base, err)
			continue
		}
		for i, row := range rows {
			cells := make([]string, 0, len(row))
			for _, cell := range row {
				if c := strings.TrimSpace(cell); c != "" {
					cells = append(cells, c)
				}
			}
			if len(cells) == 0 {
				continue
			}
			line := strings.Join(cells, " | ")
			docs = append(docs, newDoc(line,
				fmt.Sprintf("%s - %s 第 %d 行", base, sheet, i+1),
				map[string]any{MetaSheet: sheet, MetaRow: i + 1}))
		}
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("Excel 未提取到有效数据")
	}
	return docs, nil
}

// LastModified 返回文件修改时间，供调用方记录。
func LastModified(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
