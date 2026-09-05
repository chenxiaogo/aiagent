package handler

import (
	"net/http"

	"aiagent/internal/service"

	"github.com/gin-gonic/gin"
)

// AnalysisHandler 数据分析接口。
type AnalysisHandler struct {
	svc *service.AnalysisService
}

// NewAnalysisHandler 创建分析 Handler。
func NewAnalysisHandler(svc *service.AnalysisService) *AnalysisHandler {
	return &AnalysisHandler{svc: svc}
}

// RegisterRoute 注册路由。
func (h *AnalysisHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/analysis")
	{
		group.POST("/stats", h.Stats)             // 基础统计
		group.POST("/group-by", h.GroupBy)        // 分组聚合
		group.POST("/top-n", h.TopN)              // Top N
		group.POST("/correlation", h.Correlation) // 相关性
		group.POST("/line-chart", h.LineChart)    // 折线图配置
		group.POST("/bar-chart", h.BarChart)      // 柱状图配置
		group.POST("/pie-chart", h.PieChart)      // 饼图配置
		group.POST("/word-count", h.WordCount)    // 词频统计
		group.POST("/video", h.VideoAnalysis)     // 视频内容分析
	}
}

// Stats 基础统计
func (h *AnalysisHandler) Stats(c *gin.Context) {
	var req struct {
		Data []float64 `json:"data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Data) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	result := h.svc.BasicStats(req.Data)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// GroupBy 分组聚合
func (h *AnalysisHandler) GroupBy(c *gin.Context) {
	var req struct {
		Groups []string  `json:"groups"`
		Values []float64 `json:"values"`
		Agg    string    `json:"agg"` // sum / avg / count
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	var result []service.GroupValue
	switch req.Agg {
	case "avg":
		result = h.svc.GroupByAvg(req.Groups, req.Values)
	default:
		result = h.svc.GroupBySum(req.Groups, req.Values)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// TopN Top N
func (h *AnalysisHandler) TopN(c *gin.Context) {
	var req struct {
		Values []float64 `json:"values"`
		Labels []string  `json:"labels"`
		N      int       `json:"n"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.N <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	result := h.svc.TopN(req.Values, req.Labels, req.N)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// Correlation 相关性
func (h *AnalysisHandler) Correlation(c *gin.Context) {
	var req struct {
		X []float64 `json:"x"`
		Y []float64 `json:"y"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	result := h.svc.Correlation(req.X, req.Y)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// LineChart 折线图
func (h *AnalysisHandler) LineChart(c *gin.Context) {
	var req struct {
		Title  string               `json:"title"`
		XData  []string             `json:"xData"`
		Series map[string][]float64 `json:"series"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	result := h.svc.GenLineChart(req.Title, req.XData, req.Series)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// BarChart 柱状图
func (h *AnalysisHandler) BarChart(c *gin.Context) {
	var req struct {
		Title  string               `json:"title"`
		XData  []string             `json:"xData"`
		Series map[string][]float64 `json:"series"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	result := h.svc.GenBarChart(req.Title, req.XData, req.Series)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// PieChart 饼图
func (h *AnalysisHandler) PieChart(c *gin.Context) {
	var req struct {
		Title string              `json:"title"`
		Data  []service.GroupValue `json:"data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	result := h.svc.GenPieChart(req.Title, req.Data)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// WordCount 词频统计
func (h *AnalysisHandler) WordCount(c *gin.Context) {
	var req struct {
		Text string `json:"text"`
		TopN int    `json:"topN"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.TopN <= 0 {
		req.TopN = 20
	}
	result := h.svc.WordCount(req.Text, req.TopN)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// VideoAnalysis 视频内容分析
func (h *AnalysisHandler) VideoAnalysis(c *gin.Context) {
	var req service.VideoAnalysisInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	result := h.svc.AnalyzeVideoContent(&req)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}