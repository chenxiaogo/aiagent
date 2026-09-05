package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/pgvector/pgvector-go"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"
)

// EmbeddingService 向量化服务（基于 cloudwego/eino 的 Embedder 抽象，支持动态模型配置）。
type EmbeddingService struct {
	client *http.Client

	// einoEmbedders 按「apiKey|baseURL|modelName」缓存已构造的 eino Embedder。
	mu           sync.Mutex
	einoEmbedders map[string]*openai.Embedder

	// store 运行时注入，用于写调用观测日志（CallLog/embedding）
	store *store.Store
	// logEnabled 是否记录向量调用，由配置 observability.logEmbedding 控制，默认关闭
	logEnabled bool
}

// SetLogEnabled 控制是否记录向量调用观测（默认关闭，配置 observability.logEmbedding）。
func (s *EmbeddingService) SetLogEnabled(enabled bool) {
	s.logEnabled = enabled
}

// NewEmbeddingService 创建向量化服务。
func NewEmbeddingService() *EmbeddingService {
	return &EmbeddingService{
		client:        &http.Client{Timeout: 60 * time.Second},
		einoEmbedders: make(map[string]*openai.Embedder),
	}
}

// SetStore 注入数据仓库（写向量调用观测日志用）。
func (s *EmbeddingService) SetStore(st *store.Store) {
	s.store = st
}

// embedderCacheKey 生成 eino Embedder 的缓存键。
func (s *EmbeddingService) embedderCacheKey(mcfg *ModelConfig) string {
	return strings.Join([]string{mcfg.apiKey(), mcfg.baseURL(), mcfg.modelName()}, "|")
}

// buildEmbedder 基于运行时模型配置构造（或取缓存的）eino OpenAI 兼容 Embedder。
func (s *EmbeddingService) buildEmbedder(ctx context.Context, mcfg *ModelConfig) (*openai.Embedder, error) {
	if mcfg == nil || mcfg.apiKey() == "" {
		return nil, fmt.Errorf("请先在「系统设置 → 模型配置」中配置并激活一个向量模型")
	}
	key := s.embedderCacheKey(mcfg)
	s.mu.Lock()
	if e, ok := s.einoEmbedders[key]; ok {
		s.mu.Unlock()
		return e, nil
	}
	s.mu.Unlock()

	e, err := openai.NewEmbedder(ctx, &openai.EmbeddingConfig{
		APIKey:     mcfg.apiKey(),
		BaseURL:    mcfg.baseURL(),
		Model:      mcfg.modelName(),
		HTTPClient: s.client,
	})
	if err != nil {
		return nil, fmt.Errorf("构造 eino Embedder 失败: %w", err)
	}
	s.mu.Lock()
	s.einoEmbedders[key] = e
	s.mu.Unlock()
	return e, nil
}

// Embed 调用 eino Embedder 获取文本向量（批量）。
func (s *EmbeddingService) Embed(ctx context.Context, texts []string, mcfg *ModelConfig) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("empty texts")
	}
	e, err := s.buildEmbedder(ctx, mcfg)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	embeddings, err := e.EmbedStrings(ctx, texts)
	s.writeEmbedLog(ctx, mcfg, texts, time.Since(start).Milliseconds(), err)
	if err != nil {
		return nil, fmt.Errorf("eino embed: %w", err)
	}
	// eino 保证返回的向量顺序与输入一致，无需按 index 重排。
	if len(embeddings) != len(texts) {
		return nil, fmt.Errorf("embedding 数量不匹配：期望 %d，实际 %d", len(texts), len(embeddings))
	}
	return embeddings, nil
}

// writeEmbedLog 异步写入一次向量调用观测记录。
// 向量接口通常不返回 token 用量，这里按字符估算（约 1.3 字符/token），保证成本可下钻。
func (s *EmbeddingService) writeEmbedLog(ctx context.Context, mcfg *ModelConfig, texts []string, latencyMs int64, callErr error) {
	// 默认关闭：一次检索一次调用，全量记录会淹没真正需要关注的 LLM 记录
	if s.store == nil || !s.logEnabled {
		return
	}
	chars := 0
	for _, t := range texts {
		chars += len([]rune(t))
	}
	if chars <= 0 {
		return
	}
	traceID := tracex.TraceIDFromContext(ctx)
	status := 1
	errMsg := ""
	if callErr != nil {
		status = 0
		errMsg = callErr.Error()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
	}
	modelName := ""
	if mcfg != nil {
		modelName = mcfg.modelName()
	}

	go func() {
		// 落库用独立 context：调用方 ctx 可能已被取消
		bg := context.Background()
		modelID := int64(0)
		if m, e := s.store.GetActiveModelConfig(bg, model.ModelTypeEmbedding); e == nil {
			modelID = m.ID
			if modelName == "" {
				modelName = m.ModelName
			}
		}
		tokens := int(float64(chars) / 1.3)
		if err := s.store.RecordCallLog(bg, &model.CallLog{
			CallType:     model.CallTypeEmbedding,
			ModelID:      modelID,
			ModelName:    modelName,
			PromptTokens: tokens,
			TotalTokens:  tokens,
			CostCents:    s.store.EstimateCostCents(bg, modelID, int64(tokens), 0),
			LatencyMs:    latencyMs,
			Status:       status,
			ErrorMsg:     errMsg,
			TraceID:      traceID,
			CreatedAt:    time.Now(),
		}); err != nil {
			ilog.Warnf("write embedding call log: %v", err)
		}
	}()
}

// EmbedQuery 对查询文本做向量化（基于 eino Embedder）。
func (s *EmbeddingService) EmbedQuery(ctx context.Context, text string, mcfg *ModelConfig) ([]float64, error) {
	e, err := s.buildEmbedder(ctx, mcfg)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	embeddings, err := e.EmbedStrings(ctx, []string{text})
	s.writeEmbedLog(ctx, mcfg, []string{text}, time.Since(start).Milliseconds(), err)
	if err != nil {
		return nil, fmt.Errorf("eino embed query: %w", err)
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// ChunkText 将文本按 chunkSize 分块，带 overlap 重叠。
func (s *EmbeddingService) ChunkText(text string, chunkSize, overlap int) []string {
	if chunkSize <= 0 {
		chunkSize = 512
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= chunkSize {
		overlap = chunkSize / 4
	}

	runes := []rune(text)
	if len(runes) <= chunkSize {
		return []string{text}
	}

	var chunks []string
	step := chunkSize - overlap
	if step <= 0 {
		step = 1
	}

	for i := 0; i < len(runes); i += step {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunk := string(runes[i:end])
		chunk = strings.TrimSpace(chunk)
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end >= len(runes) {
			break
		}
	}

	return chunks
}

// toFloat32 将 float64 切片转为 float32 切片。
func toFloat32(f []float64) []float32 {
	r := make([]float32, len(f))
	for i, v := range f {
		r[i] = float32(v)
	}
	return r
}

// ProcessFile 处理文件：分块 + 向量化 + 存储。
func (s *EmbeddingService) ProcessFile(ctx context.Context, file *model.File, content string, mcfg *ModelConfig) error {
	// 分块
	chunks := s.ChunkText(content, 512, 64)
	if len(chunks) == 0 {
		return fmt.Errorf("no content to chunk")
	}

	ilog.Infof("file %d (%s) chunked into %d pieces", file.ID, file.FileName, len(chunks))

	// 批量向量化（每批最多 20 个）
	batchSize := 20
	var docChunks []*model.DocumentChunk
	for i := 0; i < len(chunks); i += batchSize {
		end := i + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[i:end]

		embeddings, err := s.Embed(ctx, batch, mcfg)
		if err != nil {
			ilog.Errorf("embed batch %d-%d failed: %v", i, end, err)
			continue
		}

		for j, text := range batch {
			if j >= len(embeddings) || embeddings[j] == nil {
				continue
			}
			dc := &model.DocumentChunk{
				FileID:      file.ID,
				KnowledgeID: file.KnowledgeID,
				ChunkIndex:  i + j,
				Content:     text,
				ContentLen:  len([]rune(text)),
				Embedding:   pgvector.NewVector(toFloat32(embeddings[j])),
				TokenCount:  len(embeddings[j]),
				CreatedAt:   time.Now(),
			}
			docChunks = append(docChunks, dc)
		}
		ilog.Infof("embedded batch %d-%d/%d for file %d", i, end, len(chunks), file.ID)
	}

	return nil
}
