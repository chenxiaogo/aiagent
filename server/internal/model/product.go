package model

import (
	"strings"
	"time"
)

// ---------- 商业化：产品 / 套餐 / 订阅 / 应用 ----------
//
// 商业模型：Agent 是生产物，Product 才是售卖物。
//
//	Agent ──发布──▶ Release(不可变版本) ──包装──▶ Product ──定价──▶ Plan
//	                                                  │
//	                              Subscription(客户×产品×套餐，可钉版本)
//	                                                  │
//	                                     App(客户应用) ──▶ AgentClient(API Key)
//	                                                  │
//	                                             UsageRecord(计量) ──▶ 账单

// 产品状态
const (
	ProductStatusDraft   = "draft"
	ProductStatusOnline  = "online"
	ProductStatusOffline = "offline"
)

// 订阅状态
const (
	SubscriptionStatusActive   = "active"
	SubscriptionStatusExpired  = "expired"
	SubscriptionStatusCanceled = "canceled"
)

// 交付方式（客户通过哪些渠道使用产品）
const (
	DeliveryWeb   = "web"   // 网页门户
	DeliveryAPI   = "api"   // OpenAI 兼容接口
	DeliverySDK   = "sdk"   // 语言 SDK
	DeliveryEmbed = "embed" // iframe / JS 嵌入
	DeliveryMCP   = "mcp"   // MCP 客户端接入
)

// DefaultDeliveryModes 新建产品默认开放的交付方式。
// web / sdk / embed 依赖客户门户（R2），先在库里表达，页面按支持情况标注。
func DefaultDeliveryModes() []string {
	return []string{DeliveryMCP, DeliveryAPI, DeliveryWeb}
}

// ParseDeliveryModes 解析逗号分隔的交付方式。
func ParseDeliveryModes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Product 对外售卖的 Agent 产品（与 Agent 一对一）。
type Product struct {
	ID               int64     `json:"id" gorm:"primaryKey"`
	AgentID          int64     `json:"agentId" gorm:"uniqueIndex;not null"`
	AgentSlug        string    `json:"agentSlug" gorm:"size:128;index"`
	Name             string    `json:"name" gorm:"size:128;not null"`
	Summary          string    `json:"summary" gorm:"size:512"`
	Cover            string    `json:"cover" gorm:"size:512"`
	Category         string    `json:"category" gorm:"size:64;index"`
	DeliveryModes    string    `json:"deliveryModes" gorm:"size:255"` // 逗号分隔，见 Delivery* 常量
	DefaultReleaseID int64     `json:"defaultReleaseId"`               // latest 指向；0 表示跟随 Agent 默认版本
	Status           string    `json:"status" gorm:"size:32;index;default:draft"`
	OwnerName        string    `json:"ownerName" gorm:"size:64"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// HasDeliveryMode 判断产品是否开放某种交付方式。
func (p *Product) HasDeliveryMode(mode string) bool {
	if p == nil {
		return false
	}
	for _, m := range ParseDeliveryModes(p.DeliveryModes) {
		if m == mode {
			return true
		}
	}
	return false
}

// Plan 套餐：产品的价格与配额档位。
type Plan struct {
	ID                int64     `json:"id" gorm:"primaryKey"`
	ProductID         int64     `json:"productId" gorm:"index;not null"`
	Name              string    `json:"name" gorm:"size:128;not null"`
	Code              string    `json:"code" gorm:"size:64;index"`
	PriceMonth        int64     `json:"priceMonth"` // 月单价，单位：分
	QuotaRequests     int64     `json:"quotaRequests" gorm:"default:10000"`
	QuotaTokens       int64     `json:"quotaTokens" gorm:"default:1000000"`
	QuotaMCPCalls     int64     `json:"quotaMcpCalls" gorm:"default:5000"`
	QuotaStorageMB    int64     `json:"quotaStorageMb" gorm:"default:1024"`
	QuotaVideoMinutes int64     `json:"quotaVideoMinutes" gorm:"default:60"`
	Status            int       `json:"status" gorm:"default:1;index"` // 1=启用 0=停用
	SortOrder         int       `json:"sortOrder" gorm:"default:0"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// Subscription 订阅：客户 × 产品 × 套餐，可钉住某个版本。
type Subscription struct {
	ID              int64      `json:"id" gorm:"primaryKey"`
	TenantID        int64      `json:"tenantId" gorm:"index;not null"`
	TenantName      string     `json:"tenantName" gorm:"size:128"`
	ProductID       int64      `json:"productId" gorm:"index;not null"`
	ProductName     string     `json:"productName" gorm:"size:128"`
	AgentID         int64      `json:"agentId" gorm:"index"`
	PlanID          int64      `json:"planId" gorm:"index"`
	PlanName        string     `json:"planName" gorm:"size:128"`
	PinnedReleaseID int64      `json:"pinnedReleaseId"` // 钉版本；0 = 跟随产品默认（latest）
	PinnedVersion   string     `json:"pinnedVersion" gorm:"size:32"`
	Status          string     `json:"status" gorm:"size:32;index;default:active"`
	StartedAt       *time.Time `json:"startedAt"`
	EndedAt         *time.Time `json:"endedAt"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// App 客户侧接入应用：一个订阅下可建多个应用（生产/测试），各自持有 API Key。
type App struct {
	ID              int64     `json:"id" gorm:"primaryKey"`
	TenantID        int64     `json:"tenantId" gorm:"index;not null"`
	SubscriptionID  int64     `json:"subscriptionId" gorm:"index"`
	ProductID       int64     `json:"productId" gorm:"index"`
	AgentID         int64     `json:"agentId" gorm:"index"`
	Name            string    `json:"name" gorm:"size:128;not null"`
	Env             string    `json:"env" gorm:"size:32;default:prod"` // prod / test
	DomainAllowList string    `json:"domainAllowList" gorm:"size:512"`
	Status          int       `json:"status" gorm:"default:1;index"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
