package handler

import (
	"net/http"
	"strconv"
	"time"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// ReportHandler 智能报告接口。
type ReportHandler struct {
	store *store.Store
}

// NewReportHandler 创建报告 Handler。
func NewReportHandler(s *store.Store) *ReportHandler {
	return &ReportHandler{store: s}
}

// RegisterRoute 注册路由。
func (h *ReportHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/reports")
	{
		group.GET("", h.List)
		group.POST("", h.Create)
		group.GET("/:id", h.Get)
		group.PUT("/:id", h.Update)
		group.DELETE("/:id", h.Delete)
		group.GET("/:id/html", h.DownloadHTML)
	}
}

func (h *ReportHandler) List(c *gin.Context) {
	agentID, _ := strconv.ParseInt(c.Query("agentId"), 10, 64)
	reportType := c.Query("reportType")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	reports, total, err := h.store.ListReports(tracex.FromRequest(c), agentID, reportType, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"list":     reports,
			"total":    total,
			"page":     page,
			"pageSize": pageSize,
		},
	})
}

func (h *ReportHandler) Create(c *gin.Context) {
	var req struct {
		AgentID    int64  `json:"agentId"`
		SessionID  int64  `json:"sessionId"`
		Title      string `json:"title"`
		ReportType string `json:"reportType"`
		VideoIDs   string `json:"videoIds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}

	userID, _ := c.Get("userID")
	username, _ := c.Get("username")

	report := &model.Report{
		AgentID:     req.AgentID,
		SessionID:   req.SessionID,
		Title:       req.Title,
		ReportType:  req.ReportType,
		Status:      model.ReportStatusGenerating,
		VideoIDs:    req.VideoIDs,
		CreatorID:   toInt64(userID),
		CreatorName: toString(username),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := h.store.CreateReport(tracex.FromRequest(c), report); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": report})
}

func (h *ReportHandler) Get(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	report, err := h.store.GetReport(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "报告不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": report})
}

func (h *ReportHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	report, err := h.store.GetReport(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "报告不存在"})
		return
	}
	var req struct {
		Title       *string `json:"title"`
		Status      *string `json:"status"`
		Content     *string `json:"content"`
		HTMLContent *string `json:"htmlContent"`
		Charts      *string `json:"charts"`
		ErrorMessage *string `json:"errorMessage"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Title != nil {
		report.Title = *req.Title
	}
	if req.Status != nil {
		report.Status = *req.Status
	}
	if req.Content != nil {
		report.Content = *req.Content
	}
	if req.HTMLContent != nil {
		report.HTMLContent = *req.HTMLContent
	}
	if req.Charts != nil {
		report.Charts = *req.Charts
	}
	if req.ErrorMessage != nil {
		report.ErrorMessage = *req.ErrorMessage
	}
	report.UpdatedAt = time.Now()
	if err := h.store.UpdateReport(tracex.FromRequest(c), report); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": report})
}

func (h *ReportHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteReport(tracex.FromRequest(c), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

func (h *ReportHandler) DownloadHTML(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	report, err := h.store.GetReport(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "报告不存在"})
		return
	}
	if report.HTMLContent == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "报告 HTML 内容为空"})
		return
	}
	filename := "report_" + strconv.FormatInt(id, 10) + ".html"
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.String(http.StatusOK, report.HTMLContent)
}
