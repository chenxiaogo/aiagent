package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/internal/service"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// ReindexHandler 索引重建。
//
// 三类可检索数据各有独立向量：知识库文档分块、视频场景、摄像头事件。
// 解析链路升级（如 PDF/Word 从占位文本改为真解析）或换向量模型后，
// 存量向量不会自动更新，需要在这里按类型重建。
type ReindexHandler struct {
	store *store.Store
	svc   *service.Service

	mu      sync.Mutex
	running bool
	last    *reindexRun
}

type reindexRun struct {
	StartedAt  string                  `json:"startedAt"`
	FinishedAt string                  `json:"finishedAt"`
	Running    bool                    `json:"running"`
	Results    []*service.ReindexResult `json:"results"`
}

func NewReindexHandler(s *store.Store, svc *service.Service) *ReindexHandler {
	return &ReindexHandler{store: s, svc: svc}
}

// RegisterRoute 注册索引重建路由。
func (h *ReindexHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/reindex")
	{
		group.GET("/stats", middleware.RequirePerm(model.PermRoleManage), h.Stats)
		group.POST("", middleware.RequirePerm(model.PermRoleManage), h.Run)
		group.GET("/status", middleware.RequirePerm(model.PermRoleManage), h.Status)
	}
}

// Stats 返回各类数据的规模，便于重建前确认影响面。
func (h *ReindexHandler) Stats(c *gin.Context) {
	stats, err := h.svc.Indexer.DescribeIndex(tracex.FromRequest(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": stats})
}

type reindexReq struct {
	// Types 要重建的类型：files / videos / cameras，留空表示全部
	Types      []string `json:"types"`
	AgentID    int64    `json:"agentId"`    // 限定智能体，0 表示全部
	KnowledgeID int64   `json:"knowledgeId"` // 限定知识库（仅 files），0 表示全部
}

// Run 触发重建。异步执行，用 /reindex/status 查进度。
func (h *ReindexHandler) Run(c *gin.Context) {
	var req reindexReq
	_ = c.ShouldBindJSON(&req) // 全空表示重建全部

	types := req.Types
	if len(types) == 0 {
		types = []string{service.IndexTypeFiles, service.IndexTypeVideos, service.IndexTypeCameras}
	}

	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"code": 6, "message": "已有重建任务在执行，请等待其完成"})
		return
	}
	h.running = true
	run := &reindexRun{StartedAt: time.Now().Format(time.RFC3339), Running: true, Results: []*service.ReindexResult{}}
	h.last = run
	h.mu.Unlock()

	// 用独立 context：重建可能耗时较久，不能挂在请求生命周期上
	go func() {
		ctx := tracex.NewContext(context.Background())
		defer func() {
			h.mu.Lock()
			h.running = false
			run.Running = false
			run.FinishedAt = time.Now().Format(time.RFC3339)
			h.mu.Unlock()
		}()

		for _, t := range types {
			var res *service.ReindexResult
			switch t {
			case service.IndexTypeFiles:
				res = h.svc.Indexer.ReindexFiles(ctx, req.KnowledgeID, nil)
			case service.IndexTypeVideos:
				res = h.svc.Indexer.ReindexVideoScenes(ctx, req.AgentID, nil)
			case service.IndexTypeCameras:
				res = h.svc.Indexer.ReindexCameraEvents(ctx, req.AgentID, nil)
			default:
				continue
			}
			ilog.Infof("reindex %s: total=%d ok=%d failed=%d items=%d",
				res.Type, res.Total, res.Succeeded, res.Failed, res.Items)

			h.mu.Lock()
			run.Results = append(run.Results, res)
			h.mu.Unlock()
		}
	}()

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "重建任务已启动", "data": gin.H{"types": types}})
}

// Status 查询最近一次重建任务的进度与结果。
func (h *ReindexHandler) Status(c *gin.Context) {
	h.mu.Lock()
	run := h.last
	running := h.running
	h.mu.Unlock()

	if run == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"running": false, "results": []any{}}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"running":    running,
		"startedAt":  run.StartedAt,
		"finishedAt": run.FinishedAt,
		"results":    run.Results,
	}})
}
