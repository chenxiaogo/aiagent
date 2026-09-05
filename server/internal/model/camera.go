package model

import (
	"time"

	"github.com/pgvector/pgvector-go"
)

// CameraEvent 摄像头事件记录（结构化字段 + 向量嵌入）。
type CameraEvent struct {
	ID          int64            `json:"id" gorm:"primaryKey"`
	CameraID    int64            `json:"cameraId" gorm:"index"`
	AgentID     int64            `json:"agentId" gorm:"index"` // 归属智能体：摄像头事件按智能体隔离
	KnowledgeID int64            `json:"knowledgeId" gorm:"index"` // 归属知识库：摄像头事件也隶属于某个知识库
	CameraName  string           `json:"cameraName" gorm:"size:128"`
	EventTime   time.Time        `json:"eventTime" gorm:"index"`
	Duration    float64          `json:"duration"` // 视频片段时长（秒）

	// 结构化标签
	HasPerson   bool    `json:"hasPerson" gorm:"index"`
	PersonCount int     `json:"personCount"`
	PersonDesc  string  `json:"personDesc" gorm:"size:512"` // 人物描述
	HasVehicle  bool    `json:"hasVehicle" gorm:"index"`
	VehicleType string  `json:"vehicleType" gorm:"size:64"` // car/bike/truck/motorcycle
	VehicleDesc string  `json:"vehicleDesc" gorm:"size:512"`
	HasPet      bool    `json:"hasPet" gorm:"index"`
	PetType     string  `json:"petType" gorm:"size:64"` // cat/dog/bird
	PetDesc     string  `json:"petDesc" gorm:"size:512"`
	HasPackage  bool    `json:"hasPackage" gorm:"index"`
	PackageDesc string  `json:"packageDesc" gorm:"size:512"`

	// 动作
	Action    string  `json:"action" gorm:"size:128;index"` // walking/running/stopped/picking_up/delivering
	ActionDesc string `json:"actionDesc" gorm:"size:512"`

	// 颜色
	DominantColors string `json:"dominantColors" gorm:"size:256"` // red,blue,white 逗号分隔
	ColorDesc      string `json:"colorDesc" gorm:"size:512"`

	// 区域
	Zone string `json:"zone" gorm:"size:64;index"` // entrance/yard/gate/front_door

	// 摘要
	Summary   string `json:"summary" gorm:"type:text"`        // Gemini 生成的完整描述
	JSONData  string `json:"jsonData" gorm:"type:text"`       // Gemini 返回的原始 JSON

	// 事件在视频内的大致起止秒数（视觉模型基于抽帧时间锚点估计，用于搜索结果跳转播放）
	EventStartSec float64 `json:"eventStartSec" gorm:"default:0"`
	EventEndSec   float64 `json:"eventEndSec" gorm:"default:0"`

	// 视频文件
	VideoPath string `json:"videoPath" gorm:"size:512"`       // 视频片段存储路径
	ThumbnailPath string `json:"thumbnailPath" gorm:"size:512"` // 缩略图

	// 向量嵌入（基于 summary）
	Embedding  pgvector.Vector `json:"-" gorm:"type:vector(1024)"`
	TokenCount int             `json:"tokenCount"`

	// 处理状态
	Processed     bool      `json:"processed" gorm:"index"`
	ProcessError  string    `json:"processError" gorm:"size:512"`
	CreatedAt     time.Time `json:"createdAt"`
}

// CameraEventSearchRequest 混合搜索请求
type CameraEventSearchRequest struct {
	Query       string   `json:"query"`        // 自然语言查询
	CameraID    int64    `json:"cameraId"`     // 摄像头过滤
	CameraIDs   []int64  `json:"cameraIds"`    // 多摄像头过滤
	StartTime   string   `json:"startTime"`    // 开始时间 RFC3339
	EndTime     string   `json:"endTime"`      // 结束时间
	HasPerson   *bool    `json:"hasPerson"`    // 有人
	HasVehicle  *bool    `json:"hasVehicle"`   // 有车
	HasPet      *bool    `json:"hasPet"`       // 有宠物
	HasPackage  *bool    `json:"hasPackage"`   // 有包裹
	VehicleType string   `json:"vehicleType"`  // 车型
	PetType     string   `json:"petType"`      // 宠物类型
	Action      string   `json:"action"`       // 动作
	Colors      []string `json:"colors"`       // 颜色过滤
	Zone        string   `json:"zone"`         // 区域
	AgentID     int64    `json:"agentId"`      // 归属智能体过滤
	KnowledgeID int64    `json:"knowledgeId"`  // 归属知识库过滤
	TopK        int      `json:"topK"`         // 返回数量
	Threshold   float64  `json:"threshold"`    // 相似度阈值
}

// CameraEventSearchResult 混合搜索结果
type CameraEventSearchResult struct {
	CameraEvent
	Score float64 `json:"score"` // 相似度分数
}

// GeminiVideoAnalysis Gemini 视频分析输出的 JSON 结构
type GeminiVideoAnalysis struct {
	Summary       string `json:"summary"`
	EventStartSec float64 `json:"event_start_sec"` // 事件在视频内的大致起始秒数（基于抽帧时间锚点）
	EventEndSec   float64 `json:"event_end_sec"`   // 事件在视频内的大致结束秒数
	HasPerson     bool   `json:"has_person"`
	PersonCount   int    `json:"person_count"`
	PersonDesc    string `json:"person_desc"`
	HasVehicle    bool   `json:"has_vehicle"`
	VehicleType   string `json:"vehicle_type"`
	VehicleDesc   string `json:"vehicle_desc"`
	HasPet        bool   `json:"has_pet"`
	PetType       string `json:"pet_type"`
	PetDesc       string `json:"pet_desc"`
	HasPackage    bool   `json:"has_package"`
	PackageDesc   string `json:"package_desc"`
	Action        string `json:"action"`
	ActionDesc    string `json:"action_desc"`
	DominantColors []string `json:"dominant_colors"`
	ColorDesc     string `json:"color_desc"`
	Zone          string `json:"zone"`
}