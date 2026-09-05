package service

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// AnalysisService 数据分析服务（Go 原生实现，替代 Python 沙箱）。
// 提供统计分析、数据聚合、图表配置生成等能力。
type AnalysisService struct{}

// NewAnalysisService 创建数据分析服务。
func NewAnalysisService() *AnalysisService {
	return &AnalysisService{}
}

// ---------- 基础统计 ----------

// Stats 统计结果
type Stats struct {
	Count    int     `json:"count"`
	Sum      float64 `json:"sum"`
	Mean     float64 `json:"mean"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Median   float64 `json:"median"`
	StdDev   float64 `json:"stdDev"`
	Variance float64 `json:"variance"`
	Q1       float64 `json:"q1"`
	Q3       float64 `json:"q3"`
}

// BasicStats 计算基础统计量。
func (a *AnalysisService) BasicStats(data []float64) *Stats {
	n := len(data)
	if n == 0 {
		return &Stats{}
	}

	sorted := make([]float64, n)
	copy(sorted, data)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range data {
		sum += v
	}
	mean := sum / float64(n)

	variance := 0.0
	for _, v := range data {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(n)
	stdDev := math.Sqrt(variance)

	return &Stats{
		Count:    n,
		Sum:      sum,
		Mean:     mean,
		Min:      sorted[0],
		Max:      sorted[n-1],
		Median:   percentile(sorted, 50),
		StdDev:   stdDev,
		Variance: variance,
		Q1:       percentile(sorted, 25),
		Q3:       percentile(sorted, 75),
	}
}

// percentile 计算百分位数（已排序数据）。
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	rank := (p / 100.0) * float64(n-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}
	weight := rank - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

// ---------- 分组聚合 ----------

// GroupValue 分组值
type GroupValue struct {
	Group string  `json:"group"`
	Value float64 `json:"value"`
	Count int     `json:"count,omitempty"`
}

// GroupBySum 按分组求和。
func (a *AnalysisService) GroupBySum(groups []string, values []float64) []GroupValue {
	sums := make(map[string]float64)
	counts := make(map[string]int)
	order := make([]string, 0)

	for i, g := range groups {
		if i >= len(values) {
			break
		}
		if _, ok := sums[g]; !ok {
			order = append(order, g)
		}
		sums[g] += values[i]
		counts[g]++
	}

	result := make([]GroupValue, 0, len(order))
	for _, g := range order {
		result = append(result, GroupValue{Group: g, Value: sums[g], Count: counts[g]})
	}
	return result
}

// GroupByAvg 按分组求平均。
func (a *AnalysisService) GroupByAvg(groups []string, values []float64) []GroupValue {
	sums := make(map[string]float64)
	counts := make(map[string]int)
	order := make([]string, 0)

	for i, g := range groups {
		if i >= len(values) {
			break
		}
		if _, ok := sums[g]; !ok {
			order = append(order, g)
		}
		sums[g] += values[i]
		counts[g]++
	}

	result := make([]GroupValue, 0, len(order))
	for _, g := range order {
		avg := 0.0
		if counts[g] > 0 {
			avg = sums[g] / float64(counts[g])
		}
		result = append(result, GroupValue{Group: g, Value: avg, Count: counts[g]})
	}
	return result
}

// ---------- 时间序列 ----------

// TimePoint 时间点数据
type TimePoint struct {
	Time  string  `json:"time"`
	Value float64 `json:"value"`
}

// ResampleTimeSeries 时间序列重采样（按时间段聚合）。
// period: hour/day/week/month
func (a *AnalysisService) ResampleTimeSeries(points []TimePoint, period string, agg string) []TimePoint {
	if len(points) == 0 {
		return points
	}

	buckets := make(map[string][]float64)
	order := make([]string, 0)

	for _, p := range points {
		key := truncateTime(p.Time, period)
		if _, ok := buckets[key]; !ok {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], p.Value)
	}

	sort.Strings(order)

	result := make([]TimePoint, 0, len(order))
	for _, key := range order {
		vals := buckets[key]
		var val float64
		switch agg {
		case "sum":
			for _, v := range vals {
				val += v
			}
		case "avg":
			for _, v := range vals {
				val += v
			}
			val /= float64(len(vals))
		case "max":
			val = vals[0]
			for _, v := range vals {
				if v > val {
					val = v
				}
			}
		case "min":
			val = vals[0]
			for _, v := range vals {
				if v < val {
					val = v
				}
			}
		default: // count
			val = float64(len(vals))
		}
		result = append(result, TimePoint{Time: key, Value: val})
	}
	return result
}

// truncateTime 按周期截断时间字符串（简化处理）。
func truncateTime(t string, period string) string {
	switch period {
	case "hour":
		if len(t) >= 13 {
			return t[:13] + ":00:00"
		}
	case "day":
		if len(t) >= 10 {
			return t[:10]
		}
	case "month":
		if len(t) >= 7 {
			return t[:7] + "-01"
		}
	case "year":
		if len(t) >= 4 {
			return t[:4] + "-01-01"
		}
	}
	return t
}

// ---------- Top N ----------

// TopN 返回前 N 大的值及其索引。
func (a *AnalysisService) TopN(values []float64, labels []string, n int) []GroupValue {
	type pair struct {
		label string
		value float64
	}
	pairs := make([]pair, 0, len(values))
	for i, v := range values {
		label := ""
		if i < len(labels) {
			label = labels[i]
		}
		pairs = append(pairs, pair{label, v})
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].value > pairs[j].value
	})

	if n > len(pairs) {
		n = len(pairs)
	}

	result := make([]GroupValue, n)
	for i := 0; i < n; i++ {
		result[i] = GroupValue{Group: pairs[i].label, Value: pairs[i].value}
	}
	return result
}

// ---------- 相关性 ----------

// Correlation 计算皮尔逊相关系数。
func (a *AnalysisService) Correlation(x, y []float64) float64 {
	n := len(x)
	if n == 0 || n != len(y) {
		return 0
	}

	var sumX, sumY, sumXY, sumX2, sumY2 float64
	for i := 0; i < n; i++ {
		sumX += x[i]
		sumY += y[i]
		sumXY += x[i] * y[i]
		sumX2 += x[i] * x[i]
		sumY2 += y[i] * y[i]
	}

	numerator := float64(n)*sumXY - sumX*sumY
	denominator := math.Sqrt((float64(n)*sumX2 - sumX*sumX) * (float64(n)*sumY2 - sumY*sumY))

	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

// ---------- 图表配置生成 ----------

// EChartsOption ECharts 配置
type EChartsOption struct {
	Title  map[string]interface{}   `json:"title,omitempty"`
	Tooltip map[string]interface{}   `json:"tooltip,omitempty"`
	Legend map[string]interface{}    `json:"legend,omitempty"`
	XAxis  map[string]interface{}   `json:"xAxis,omitempty"`
	YAxis  map[string]interface{}   `json:"yAxis,omitempty"`
	Series []map[string]interface{} `json:"series,omitempty"`
	Grid   map[string]interface{}   `json:"grid,omitempty"`
}

// GenLineChart 生成折线图配置。
func (a *AnalysisService) GenLineChart(title string, xData []string, series map[string][]float64) string {
	opt := EChartsOption{
		Title:  map[string]interface{}{"text": title, "left": "center"},
		Tooltip: map[string]interface{}{"trigger": "axis"},
		Legend:  map[string]interface{}{"bottom": 0},
		XAxis:   map[string]interface{}{"type": "category", "data": xData},
		YAxis:   map[string]interface{}{"type": "value"},
		Grid:    map[string]interface{}{"bottom": 40, "top": 50},
		Series:  make([]map[string]interface{}, 0, len(series)),
	}
	for name, data := range series {
		opt.Series = append(opt.Series, map[string]interface{}{
			"name": name, "type": "line", "data": data, "smooth": true,
		})
	}
	b, _ := json.Marshal(opt)
	return string(b)
}

// GenBarChart 生成柱状图配置。
func (a *AnalysisService) GenBarChart(title string, xData []string, series map[string][]float64) string {
	opt := EChartsOption{
		Title:   map[string]interface{}{"text": title, "left": "center"},
		Tooltip: map[string]interface{}{"trigger": "axis"},
		Legend:  map[string]interface{}{"bottom": 0},
		XAxis:   map[string]interface{}{"type": "category", "data": xData},
		YAxis:   map[string]interface{}{"type": "value"},
		Grid:    map[string]interface{}{"bottom": 40, "top": 50},
		Series:  make([]map[string]interface{}, 0, len(series)),
	}
	for name, data := range series {
		opt.Series = append(opt.Series, map[string]interface{}{
			"name": name, "type": "bar", "data": data,
		})
	}
	b, _ := json.Marshal(opt)
	return string(b)
}

// GenPieChart 生成饼图配置。
func (a *AnalysisService) GenPieChart(title string, data []GroupValue) string {
	pieData := make([]map[string]interface{}, 0, len(data))
	for _, d := range data {
		pieData = append(pieData, map[string]interface{}{
			"name": d.Group, "value": d.Value,
		})
	}
	opt := map[string]interface{}{
		"title":   map[string]interface{}{"text": title, "left": "center"},
		"tooltip": map[string]interface{}{"trigger": "item"},
		"legend":  map[string]interface{}{"bottom": 0},
		"series": []map[string]interface{}{
			{
				"name": title, "type": "pie", "radius": "50%",
				"data": pieData,
				"emphasis": map[string]interface{}{
					"itemStyle": map[string]interface{}{
						"shadowBlur": 10, "shadowOffsetX": 0, "shadowColor": "rgba(0, 0, 0, 0.5)",
					},
				},
			},
		},
	}
	b, _ := json.Marshal(opt)
	return string(b)
}

// GenTableData 生成表格数据（二维数组）。
func (a *AnalysisService) GenTableData(headers []string, rows [][]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"headers": headers,
		"rows":    rows,
	}
}

// ---------- 文本分析 ----------

// WordCount 词频统计（中文按字符，英文按空格分词）。
func (a *AnalysisService) WordCount(text string, topN int) []GroupValue {
	text = strings.ToLower(text)
	words := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == '!' || r == '?' || r == '\n' || r == '\t' || r == '，' || r == '。' || r == '！' || r == '？'
	})

	counts := make(map[string]int)
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w != "" && len([]rune(w)) > 1 {
			counts[w]++
		}
	}

	type pair struct {
		word  string
		count int
	}
	pairs := make([]pair, 0, len(counts))
	for w, c := range counts {
		pairs = append(pairs, pair{w, c})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].count > pairs[j].count
	})

	if topN > len(pairs) {
		topN = len(pairs)
	}

	result := make([]GroupValue, topN)
	for i := 0; i < topN; i++ {
		result[i] = GroupValue{Group: pairs[i].word, Value: float64(pairs[i].count)}
	}
	return result
}

// ---------- 视频内容分析 ----------

// VideoAnalysisInput 视频分析输入
type VideoAnalysisInput struct {
	VideoTitle   string            `json:"videoTitle"`
	Transcript   string            `json:"transcript"`
	Summary      string            `json:"summary"`
	Scenes       []string          `json:"scenes"`
	SceneTimes   []float64         `json:"sceneTimes"`
	UserQuestion string            `json:"userQuestion"`
	AnalysisType string            `json:"analysisType"` // summary/keywords/sentiment/topics
}

// VideoAnalysisResult 视频分析结果
type VideoAnalysisResult struct {
	Title       string        `json:"title"`
	Summary     string        `json:"summary"`
	KeyPoints   []string      `json:"keyPoints"`
	Keywords    []GroupValue  `json:"keywords"`
	Topics      []string      `json:"topics"`
	Stats       *Stats        `json:"stats,omitempty"`
	Charts      []string      `json:"charts"` // ECharts 配置 JSON 列表
	TableData   interface{}   `json:"tableData,omitempty"`
}

// AnalyzeVideoContent 执行视频内容分析（纯 Go 实现，无需 Python）。
func (a *AnalysisService) AnalyzeVideoContent(input *VideoAnalysisInput) *VideoAnalysisResult {
	result := &VideoAnalysisResult{
		Title:   input.VideoTitle,
		Summary: input.Summary,
	}

	// 关键词提取（基于词频）
	result.Keywords = a.WordCount(input.Transcript, 20)

	// 提取要点（基于句子分割和关键词权重）
	result.KeyPoints = extractKeyPoints(input.Transcript, 5)

	// 主题提取（简化：基于高频词聚类）
	result.Topics = extractTopics(input.Transcript, 3)

	// 生成词云图（用柱状图替代）
	if len(result.Keywords) > 0 {
		topKeywords := result.Keywords
		if len(topKeywords) > 10 {
			topKeywords = topKeywords[:10]
		}
		labels := make([]string, len(topKeywords))
		values := make([]float64, len(topKeywords))
		for i, kw := range topKeywords {
			labels[i] = kw.Group
			values[i] = kw.Value
		}
		chart := a.GenBarChart("高频关键词", labels, map[string][]float64{"出现次数": values})
		result.Charts = append(result.Charts, chart)
	}

	// 场景时长分布（如果有场景数据）
	if len(input.Scenes) > 0 && len(input.SceneTimes) > 0 {
		sceneLabels := make([]string, len(input.Scenes))
		for i := range input.Scenes {
			sceneLabels[i] = fmt.Sprintf("场景%d", i+1)
		}
		durations := make([]float64, len(input.SceneTimes))
		copy(durations, input.SceneTimes)
		chart := a.GenBarChart("场景时长分布", sceneLabels, map[string][]float64{"时长(秒)": durations})
		result.Charts = append(result.Charts, chart)
	}

	return result
}

// extractKeyPoints 提取关键句子。
func extractKeyPoints(text string, n int) []string {
	// 简单实现：按句号/感叹号/问号分割，取最长的 n 句
	separators := []string{"。", "！", "？", ".", "!", "?"}
	sentences := []string{}
	current := ""

	for _, r := range text {
		current += string(r)
		for _, sep := range separators {
			if strings.HasSuffix(current, sep) {
				s := strings.TrimSpace(current)
				if len([]rune(s)) > 10 {
					sentences = append(sentences, s)
				}
				current = ""
				break
			}
		}
	}
	if strings.TrimSpace(current) != "" {
		sentences = append(sentences, strings.TrimSpace(current))
	}

	// 按长度排序（长句子通常包含更多信息）
	sort.Slice(sentences, func(i, j int) bool {
		return len([]rune(sentences[i])) > len([]rune(sentences[j]))
	})

	if n > len(sentences) {
		n = len(sentences)
	}
	if n <= 0 {
		return []string{}
	}
	return sentences[:n]
}

// extractTopics 提取主题（简化版：取高频词组合）。
func extractTopics(text string, n int) []string {
	// 简化：取 Top N 关键词作为主题
	keywords := (&AnalysisService{}).WordCount(text, n*3)
	topics := make([]string, 0, n)
	for i, kw := range keywords {
		if i >= n {
			break
		}
		topics = append(topics, kw.Group)
	}
	return topics
}
