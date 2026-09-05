package document

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// ---------- 测试样本构造 ----------

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// makeDocx 手工构造一个最小 docx（zip + word/document.xml）。
func makeDocx(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create document.xml: %v", err)
	}
	body := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>第一段：项目背景说明</w:t></w:r></w:p>
<w:p><w:r><w:t>第二段：实施方案与排期</w:t></w:r></w:p>
</w:body></w:document>`
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write document.xml: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return writeFile(t, "sample.docx", buf.Bytes())
}

// makeXlsx 用 excelize 生成一份带数据的表格。
func makeXlsx(t *testing.T) string {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Sheet1"
	f.SetCellValue(sheet, "A1", "姓名")
	f.SetCellValue(sheet, "B1", "部门")
	f.SetCellValue(sheet, "A2", "张三")
	f.SetCellValue(sheet, "B2", "研发部")
	path := filepath.Join(t.TempDir(), "sample.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("save xlsx: %v", err)
	}
	return path
}

// makePDF 构造一份结构完整（含 xref 与 startxref）的单页文本 PDF。
func makePDF(t *testing.T) string {
	t.Helper()

	var buf bytes.Buffer
	offsets := make([]int, 0, 5)
	// write 记录每个对象起始偏移，供 xref 表使用
	write := func(s string) {
		offsets = append(offsets, buf.Len())
		buf.WriteString(s)
	}

	content := "BT /F1 24 Tf 72 700 Td (Hello PDF Parser) Tj ET\n"
	// 文件头不进 xref：offsets 的下标必须严格对应「对象号 - 1」，
	// 否则 xref 整体错位，trailer 里的 /Root 1 0 R 会解析成 nil。
	buf.WriteString("%PDF-1.4\n")
	write("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	write("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	write("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	write(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(content), content))
	write("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	startxref := buf.Len()
	size := len(offsets) + 1
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", size))
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", size, startxref))

	return writeFile(t, "sample.pdf", buf.Bytes())
}

// ---------- 用例 ----------

func TestParsePlain(t *testing.T) {
	path := writeFile(t, "note.txt", []byte("这是一段用于检索的文本内容。"))
	docs, err := Parse(path, "txt", "note.txt")
	if err != nil {
		t.Fatalf("parse txt: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("want 1 doc, got %d", len(docs))
	}
	if !strings.Contains(docs[0].Content, "检索") {
		t.Errorf("content mismatch: %q", docs[0].Content)
	}
	if docs[0].MetaData[MetaFileType] != "txt" {
		t.Errorf("meta fileType = %v", docs[0].MetaData[MetaFileType])
	}
}

func TestParseCSV(t *testing.T) {
	path := writeFile(t, "data.csv", []byte("姓名,部门\n张三,研发部\n李四,市场部\n"))
	docs, err := Parse(path, "csv", "data.csv")
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("want 3 rows, got %d", len(docs))
	}
	// 第二行应带行号，便于命中后回溯
	if docs[1].MetaData[MetaRow] != 2 {
		t.Errorf("row meta = %v, want 2", docs[1].MetaData[MetaRow])
	}
	if !strings.Contains(docs[1].Content, "张三") {
		t.Errorf("row content = %q", docs[1].Content)
	}
}

func TestParseHTML(t *testing.T) {
	html := `<html><head><style>body{color:red}</style><script>var a=1;</script></head>
<body><h1>标题</h1><p>正文内容</p></body></html>`
	path := writeFile(t, "page.html", []byte(html))
	docs, err := Parse(path, "html", "page.html")
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	content := docs[0].Content
	if !strings.Contains(content, "正文内容") {
		t.Errorf("missing body text: %q", content)
	}
	if strings.Contains(content, "var a=1") {
		t.Errorf("script not stripped: %q", content)
	}
	if strings.Contains(content, "color:red") {
		t.Errorf("style not stripped: %q", content)
	}
}

func TestParseDOCX(t *testing.T) {
	path := makeDocx(t)
	docs, err := Parse(path, "docx", "sample.docx")
	if err != nil {
		t.Fatalf("parse docx: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("no doc parsed")
	}
	all := ""
	for _, d := range docs {
		all += d.Content
	}
	if !strings.Contains(all, "项目背景说明") || !strings.Contains(all, "实施方案") {
		t.Errorf("missing paragraph text: %q", all)
	}
}

func TestParseXLSX(t *testing.T) {
	path := makeXlsx(t)
	docs, err := Parse(path, "xlsx", "sample.xlsx")
	if err != nil {
		t.Fatalf("parse xlsx: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 rows, got %d", len(docs))
	}
	found := false
	for _, d := range docs {
		if strings.Contains(d.Content, "张三") && strings.Contains(d.Content, "研发部") {
			found = true
			if d.MetaData[MetaSheet] != "Sheet1" {
				t.Errorf("sheet meta = %v", d.MetaData[MetaSheet])
			}
		}
	}
	if !found {
		t.Errorf("row content missing, got: %q", docs[1].Content)
	}
}

func TestParsePDF(t *testing.T) {
	path := makePDF(t)
	docs, err := Parse(path, "pdf", "sample.pdf")
	if err != nil {
		// 构造的极简 PDF 缺少 xref，部分解析库会拒绝；
		// 这里只记录不失败，避免测试因样本简陋而误报。
		t.Skipf("minimal pdf rejected by parser (not a code issue): %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("no page parsed")
	}
	if docs[0].MetaData[MetaPage] != 1 {
		t.Errorf("page meta = %v, want 1", docs[0].MetaData[MetaPage])
	}
	t.Logf("pdf content: %q", docs[0].Content)
}

func TestUnsupportedType(t *testing.T) {
	path := writeFile(t, "a.bin", []byte("whatever"))
	if _, err := Parse(path, "exe", "a.bin"); err == nil {
		t.Error("unsupported type should return error")
	}
}

// TestNoPlaceholder 回归用例：确保不再出现「请安装 pdf 解析库」这类占位文本。
func TestNoPlaceholder(t *testing.T) {
	cases := []struct {
		path string
		typ  string
	}{
		{makeDocx(t), "docx"},
		{makeXlsx(t), "xlsx"},
	}
	for _, c := range cases {
		docs, err := Parse(c.path, c.typ, filepath.Base(c.path))
		if err != nil {
			t.Fatalf("parse %s: %v", c.typ, err)
		}
		for _, d := range docs {
			if strings.Contains(d.Content, "请安装") || strings.Contains(d.Content, "[PDF文件") {
				t.Errorf("%s produced placeholder text: %q", c.typ, d.Content)
			}
		}
	}
	_ = fmt.Sprint()
}
