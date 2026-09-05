package knowledge

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"

	agentscope "aiagent/internal/agent"
	"aiagent/internal/model"
	"aiagent/internal/store"
)

type EmbedQueryFunc func(ctx context.Context, query string) ([]float64, error)

// Retriever 封装知识库检索并强制执行 AgentResource 边界，同时实现 Eino Retriever。
// rewriteQuery / rerank / aggregate 为可选的检索增强环节，由组装层按需注入（依赖 LLM），
// 不注入时回落到「原 query 直检 → 现有加权排序 → 原文截断」的朴素链路，保证可用性。
type Retriever struct {
	store        *store.Store
	embedQuery   EmbedQueryFunc
	rewriteQuery func(ctx context.Context, query string) (string, error)                                             // 可选：查询改写（口语/模糊 → 精准检索 query）
	rerank       func(ctx context.Context, query string, docs []*schema.Document) ([]*schema.Document, error)      // 可选：结果重排
	aggregate    func(ctx context.Context, query string, docs []*schema.Document) (string, error)                  // 可选：聚合压缩为带出处的要点
}

// Option 配置 Retriever 的可选检索增强能力。
type Option func(*Retriever)

// WithRewriteQuery 注入查询改写函数（用 LLM 把用户问题改写成更利于召回的检索 query）。
func WithRewriteQuery(fn func(ctx context.Context, query string) (string, error)) Option {
	return func(r *Retriever) { r.rewriteQuery = fn }
}

// WithRerank 注入结果重排函数（如有 rerank 模型则对召回做交叉编码重排，否则不启用）。
func WithRerank(fn func(ctx context.Context, query string, docs []*schema.Document) ([]*schema.Document, error)) Option {
	return func(r *Retriever) { r.rerank = fn }
}

// WithAggregate 注入聚合函数（用 LLM 把命中片段提炼成带出处的要点，缓解「内容模糊」）。
func WithAggregate(fn func(ctx context.Context, query string, docs []*schema.Document) (string, error)) Option {
	return func(r *Retriever) { r.aggregate = fn }
}

func NewRetriever(st *store.Store, embedQuery EmbedQueryFunc, opts ...Option) *Retriever {
	r := &Retriever{store: st, embedQuery: embedQuery}
	for _, o := range opts {
		o(r)
	}
	return r
}

func (r *Retriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	scope, err := agentscope.RequireScope(ctx)
	if err != nil {
		return nil, err
	}
	kbIDs := append([]int64(nil), scope.KnowledgeBaseIDs...)
	if len(kbIDs) == 0 {
		kbIDs, err = r.store.ListBoundResourceIDs(ctx, scope.AgentID, model.ResourceTypeKnowledgeBase)
		if err != nil {
			return nil, fmt.Errorf("加载智能体知识库授权失败: %w", err)
		}
	}
	if len(kbIDs) == 0 {
		return nil, nil
	}
	options := retriever.GetCommonOptions(nil, opts...)
	topK := 5
	threshold := 0.45
	if options.TopK != nil && *options.TopK > 0 {
		topK = *options.TopK
	}
	if options.ScoreThreshold != nil {
		threshold = *options.ScoreThreshold
	}

	// 多召回一倍：下面的清洗会淘汰噪声与重复片段，按 topK 召回会导致最终条数不足
	recallTopK := topK * 2
	var results []model.SearchResult
	if r.embedQuery != nil {
		vector, embedErr := r.embedQuery(ctx, query)
		if embedErr == nil {
			results, err = r.store.HybridSearchInKBs(ctx, vector, query, kbIDs, recallTopK, threshold)
		} else {
			results, err = r.store.FullTextSearchInKBs(ctx, query, kbIDs, recallTopK)
		}
	} else {
		results, err = r.store.FullTextSearchInKBs(ctx, query, kbIDs, recallTopK)
	}
	if err != nil {
		return nil, err
	}

	// 检索结果统一清洗后再交给 Agent：脏片段（页眉页脚、分隔线、重复入库的段落）
	// 进了提示词只是白占上下文，还可能把模型带偏。
	results = CleanSearchResults(results, topK)

	documents := make([]*schema.Document, 0, len(results))
	for _, result := range results {
		documents = append(documents, &schema.Document{
			ID:      strconv.FormatInt(result.ChunkID, 10),
			Content: result.Content,
			MetaData: map[string]any{
				"file_id": result.FileID, "file_name": result.FileName,
				"chunk_index": result.ChunkIndex, "score": result.Score,
				// 分块元数据（页码 / 行号 / 工作表名），Agent 引用出处时可用
				"metadata": result.Metadata,
			},
		})
	}
	return documents, nil
}

type SearchInput struct {
	Query     string  `json:"query" jsonschema:"description=要检索的知识问题,required"`
	TopK      int     `json:"top_k,omitempty" jsonschema:"description=返回结果数量"`
	Threshold float64 `json:"threshold,omitempty" jsonschema:"description=最低相似度阈值"`
}

// SearchText 执行知识库检索并返回适合 Agent observation 的压缩文本。
// 检索增强链路（各环节均可选）：查询改写 → 混合检索 → 重排 → 聚合压缩（带出处）。
func (r *Retriever) SearchText(ctx context.Context, input SearchInput) (string, error) {
	if strings.TrimSpace(input.Query) == "" {
		return "", fmt.Errorf("query is required")
	}
	if input.TopK <= 0 || input.TopK > 10 {
		input.TopK = 5
	}
	if input.Threshold <= 0 {
		input.Threshold = 0.3
	}

	// 1) 查询改写（可选）：把口语化/模糊问题改写成利于向量召回的精准 query。
	//    失败不致命，回落原 query，避免改写链路异常阻断检索。
	query := input.Query
	if r.rewriteQuery != nil {
		if rewritten, err := r.rewriteQuery(ctx, query); err == nil && strings.TrimSpace(rewritten) != "" {
			query = strings.TrimSpace(rewritten)
		}
	}

	docs, err := r.Retrieve(ctx, query, retriever.WithTopK(input.TopK), retriever.WithScoreThreshold(input.Threshold))
	if err != nil {
		return "", err
	}
	if len(docs) == 0 {
		return "当前智能体未绑定知识库，或授权知识库中未找到与问题相关的内容。", nil
	}

	// 2) 重排（可选）：有 rerank 模型则交叉编码重排；否则沿用混合检索已有的加权打分排序。
	if r.rerank != nil {
		if reranked, err := r.rerank(ctx, query, docs); err == nil && len(reranked) > 0 {
			docs = reranked
		}
	}

	// 3) 聚合压缩（可选）：用 LLM 把片段提炼成带出处的要点；失败回落到原格式化。
	if r.aggregate != nil {
		if summary, err := r.aggregate(ctx, query, docs); err == nil && strings.TrimSpace(summary) != "" {
			return summary, nil
		}
	}

	// 兜底：原文截断 + 文件名，保证无 LLM 时也能给出可用结果。
	var b strings.Builder
	fmt.Fprintf(&b, "找到 %d 条授权知识库内容（检索查询：%s）：", len(docs), query)
	for i, doc := range docs {
		fileName, _ := doc.MetaData["file_name"].(string)
		fmt.Fprintf(&b, "\n[%d] %s\n%s", i+1, fileName, truncate(doc.Content, 600))
	}
	return b.String(), nil
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

// DescribeChunkMeta 把分块元数据（页码/行号/工作表名等）转成简短出处描述，供聚合提示词引用。
func DescribeChunkMeta(raw any) string {
	m, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, key := range []string{"page", "line", "row", "sheet", "start", "chapter", "section"} {
		if v, ok := m[key]; ok && v != nil {
			parts = append(parts, fmt.Sprintf("%s:%v", key, v))
		}
	}
	return strings.Join(parts, " ")
}
