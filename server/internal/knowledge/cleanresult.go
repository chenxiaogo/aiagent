package knowledge

import (
	"aiagent/internal/document"
	"aiagent/internal/model"
)

// CleanSearchResults 清洗检索结果，是所有检索出口（Agent 工具 / 智能检索页）的统一收口。
//
// 做三件事：
//  1. 文本清洗：抹掉页眉页脚、分隔线、HTML 残留等解析噪声，避免脏文本进 Agent 上下文；
//  2. 噪声过滤：清洗后为空或有效字符占比过低的分块直接丢掉（它们大多是孤立页码）；
//  3. 指纹去重：内容相同的片段只保留第一条 —— 重复入库的文件、重叠分块会
//     让 Agent 的 observation 里塞满重复段落，白白吃掉上下文预算。
//
// limit <= 0 表示不限条数。原顺序（已按相关度排好）保持不变。
func CleanSearchResults(results []model.SearchResult, limit int) []model.SearchResult {
	out := make([]model.SearchResult, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, r := range results {
		text := document.CleanText(r.Content)
		if text == "" || document.IsLowQuality(text) {
			continue
		}
		fp := document.Fingerprint(text)
		if _, ok := seen[fp]; ok {
			continue
		}
		seen[fp] = struct{}{}
		r.Content = text
		out = append(out, r)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
