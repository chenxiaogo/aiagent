package store

import (
	"context"

	"github.com/pgvector/pgvector-go"

	"aiagent/internal/model"
)

// ---------- 重建索引所需的数据访问 ----------
//
// 三类可检索数据各有独立的向量列：
//   - document_chunks：知识库文档分块（PDF/Word/Excel/TXT 解析后）
//   - video_scenes   ：视频场景（描述 + 字幕）
//   - camera_events  ：摄像头事件（AI 摘要）
//
// 解析链路变更、更换向量模型、或既有向量损坏时，需要按类型批量重建。

// ListFilesForReindex 列出待重建索引的文件。knowledgeID 为 0 表示全部。
func (s *Store) ListFilesForReindex(ctx context.Context, knowledgeID int64) ([]*model.File, error) {
	var list []*model.File
	q := s.db.WithContext(ctx).Model(&model.File{})
	if knowledgeID > 0 {
		q = q.Where("knowledge_id = ?", knowledgeID)
	}
	if err := q.Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListVideoScenesForReindex 列出待重建向量的视频场景。
// 只取描述或字幕非空的 —— 空的没内容可向量化。agentID 为 0 表示全部。
func (s *Store) ListVideoScenesForReindex(ctx context.Context, agentID int64) ([]*model.VideoScene, error) {
	var list []*model.VideoScene
	q := s.db.WithContext(ctx).Model(&model.VideoScene{}).
		Where("(description IS NOT NULL AND description <> '') OR (transcript IS NOT NULL AND transcript <> '')")
	if agentID > 0 {
		q = q.Where("agent_id = ?", agentID)
	}
	if err := q.Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListCameraEventsForReindex 列出待重建向量的摄像头事件。
// 只取已处理且摘要非空的 —— 未处理的还没有可向量化的文本。agentID 为 0 表示全部。
func (s *Store) ListCameraEventsForReindex(ctx context.Context, agentID int64) ([]*model.CameraEvent, error) {
	var list []*model.CameraEvent
	q := s.db.WithContext(ctx).Model(&model.CameraEvent{}).
		Where("processed = ? AND (summary IS NOT NULL AND summary <> '')", true)
	if agentID > 0 {
		q = q.Where("agent_id = ?", agentID)
	}
	if err := q.Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateSceneEmbedding 只更新视频场景的向量列，避免整行 Save 覆盖其它字段。
func (s *Store) UpdateSceneEmbedding(ctx context.Context, id int64, vec pgvector.Vector, tokenCount int) error {
	return s.db.WithContext(ctx).Model(&model.VideoScene{}).Where("id = ?", id).
		Updates(map[string]interface{}{"embedding": vec, "token_count": tokenCount}).Error
}

// UpdateCameraEventEmbedding 只更新摄像头事件的向量列。
func (s *Store) UpdateCameraEventEmbedding(ctx context.Context, id int64, vec pgvector.Vector, tokenCount int) error {
	return s.db.WithContext(ctx).Model(&model.CameraEvent{}).Where("id = ?", id).
		Updates(map[string]interface{}{"embedding": vec, "token_count": tokenCount}).Error
}

// CountChunks 统计既有分块数，供重建前后对比。
func (s *Store) CountChunks(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.DocumentChunk{}).Count(&n).Error
	return n, err
}
