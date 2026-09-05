package document

import (
	"regexp"
	"strings"
	"unicode"
)

// 解析产物里常见的噪声，统一在这里收口：
//   - PDF 抽取会带出页眉页脚（每页重复的短行）与分隔线（---- / ====）；
//   - HTML 解析会残留标签与 script / style；
//   - CSV / Excel 转换会带出大量连续空行与全角空格。
//
// 不清洗的后果不是「难看」，而是污染向量：噪声文本也会被 Embedding 写进 pgvector，
// 检索时以高分命中，Agent 拿到的 observation 里全是 "第 3 页"、"-----"。
var (
	// 零宽字符与 BOM：肉眼不可见，但会进向量、会让字符串比对永远不等
	zeroWidthRe = regexp.MustCompile(`[\x{200B}-\x{200D}\x{FEFF}]`)
	// 各类空白（连续空格 / 制表 / 不间断空格 / 全角空格）压成一个半角空格。
	// 必须是双引号字符串：RE2 不认 \uXXXX，要靠 Go 先把转义解成真实 rune。
	spaceRe = regexp.MustCompile("[ \t\u00A0\u3000]+")
	// 三个以上连续换行压成两个（保留一个空行作为段落分隔）
	blankLineRe = regexp.MustCompile(`\n{3,}`)
	// 纯分隔符行：---- / ==== / ____ / ···· 等
	separatorRe = regexp.MustCompile(`^[\s\-_=*·•.。、|/\\#~+]{3,}$`)
	// 残留的 HTML 标签与 script / style 块。
	// script 与 style 各写一个正则：RE2 不支持反向引用，写不成 <(script|style)>...</\1>。
	scriptRe  = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	styleRe   = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	htmlTagRe = regexp.MustCompile(`<[^<>\s]{1,64}>`)
)

// 页眉页脚判定参数。
//
// 页眉页脚的特点不是「相邻重复」，而是「在全文反复出现且很短」——
// 每页页脚的页码还各不相同（第 1 页 / 第 2 页），只有页眉文字是固定的。
// 所以按频次判定，而不是按相邻行比对。
const (
	// MaxNoiseLineRunes 参与页眉页脚判定的行长度上限
	MaxNoiseLineRunes = 40
	// HeaderFooterRepeat 短行出现次数达到该值即判为页眉页脚
	HeaderFooterRepeat = 3
	// MinLinesForHeaderFooter 文档行数下限：太短的文档不做频次判定，避免误删
	MinLinesForHeaderFooter = 6
)

// MinChunkRunes 低于该长度的文本视为噪声（孤立页码、"第 1 页"、单个词）。
const MinChunkRunes = 8

// MinValidRatio 有效字符（字母 / 数字 / 文字）占比下限，低于此值判定为噪声块。
const MinValidRatio = 0.2

// CleanText 规范化解析出的文本：去不可见字符、压平空白、剔除分隔线与连续重复短行。
//
// 只做无损语义的整理，不做内容裁剪 —— 清洗后仍为空或低质量的块由调用方用
// IsLowQuality 过滤，两件事分开，便于单独调整阈值。
func CleanText(s string) string {
	if s == "" {
		return ""
	}

	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = zeroWidthRe.ReplaceAllString(s, "")
	s = scriptRe.ReplaceAllString(s, " ")
	s = styleRe.ReplaceAllString(s, " ")
	s = htmlTagRe.ReplaceAllString(s, " ")

	// 第一轮：基础清洗（压空白、去分隔线），同时统计短行频次供页眉页脚判定
	raw := strings.Split(s, "\n")
	lines := make([]string, 0, len(raw))
	freq := make(map[string]int, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(spaceRe.ReplaceAllString(line, " "))
		// 分隔线整行丢掉，连空行都不留：留空行会把正文切成一段一段
		if separatorRe.MatchString(line) {
			continue
		}
		if line == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, line)
		if len([]rune(line)) <= MaxNoiseLineRunes {
			freq[line]++
		}
	}

	// 第二轮：丢掉页眉页脚与连续重复行
	shortDoc := len(lines) < MinLinesForHeaderFooter
	out := make([]string, 0, len(lines))
	prev := ""
	for _, line := range lines {
		if line == "" {
			out = append(out, "")
			continue
		}
		if !shortDoc && len([]rune(line)) <= MaxNoiseLineRunes && freq[line] >= HeaderFooterRepeat {
			continue
		}
		// 连续完全相同的短行：HTML 导航残留的典型特征。
		// 只对短行生效，避免把表格里连续两行相同取值（如两行都是"是"）误删。
		if line == prev && len([]rune(line)) <= MaxNoiseLineRunes {
			continue
		}
		prev = line
		out = append(out, line)
	}

	return strings.TrimSpace(blankLineRe.ReplaceAllString(strings.Join(out, "\n"), "\n\n"))
}

// IsLowQuality 判断清洗后的文本是否仍是不该进向量库的噪声块。
// 判定：过短，或有效字符（字母 / 数字 / 文字）占比过低。
func IsLowQuality(s string) bool {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) < MinChunkRunes {
		return true
	}
	valid := 0
	for _, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			valid++
		}
	}
	return float64(valid)/float64(len(runes)) < MinValidRatio
}

// Fingerprint 计算文本指纹：去掉所有空白后取前 64 个字符。
// 用于检索结果去重 —— 同一份文件被重复入库、或相邻分块高度重叠时，
// 指纹相同的片段对 Agent 而言是重复信息，只会白白吃掉上下文预算。
func Fingerprint(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= 64 {
			break
		}
	}
	return b.String()
}
