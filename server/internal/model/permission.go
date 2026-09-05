package model

import "time"

// ---------- 菜单与按钮（迁移自 scheduler-platform，参考 gin-vue-admin 权限体系） ----------

// Menu 菜单表。树形结构，path/component 供前端动态路由与侧边栏渲染使用。
type Menu struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	ParentID  int64     `json:"parentId" gorm:"index;default:0"` // 父菜单ID，0 为顶级
	Name      string    `json:"name" gorm:"size:64;index"`       // 路由 name
	Path      string    `json:"path" gorm:"size:128"`            // 路由 path
	Component string    `json:"component" gorm:"size:128"`       // 对应前端文件路径，如 view/agent/AgentList
	Title     string    `json:"title" gorm:"size:64"`            // 菜单名
	Icon      string    `json:"icon" gorm:"size:64"`             // 菜单图标
	Sort      int       `json:"sort" gorm:"default:0"`           // 排序
	Hidden    bool      `json:"hidden"`                          // 是否在侧边栏隐藏
	Type      string    `json:"type" gorm:"size:16"`             // menu / dir（目录）/ button
	PermCode  string    `json:"permCode" gorm:"size:64;index"`   // 关联的权限码（如 task:view），用于菜单权限校验
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// 非 DB 字段
	Children []*Menu    `json:"children" gorm:"-"`
	Btns     []*MenuBtn `json:"btns" gorm:"-"`   // 该角色在该菜单下可用的按钮
	BtnAll   []*MenuBtn `json:"btnAll" gorm:"-"` // 该菜单下所有可配置按钮
}

// MenuBtn 菜单按钮定义。属于某菜单，Name 为按钮权限 key（如 add/edit/delete）。
type MenuBtn struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	MenuID    int64     `json:"menuId" gorm:"index"`
	Name      string    `json:"name" gorm:"size:64"`     // 按钮关键 key，如 add/edit/delete/run
	Desc      string    `json:"desc" gorm:"size:128"`    // 按钮备注
	PermCode  string    `json:"permCode" gorm:"size:64"` // 关联的权限码（如 task:create）
	CreatedAt time.Time `json:"createdAt"`
}

// RoleMenu 角色-菜单多对多关联。
type RoleMenu struct {
	ID     int64 `json:"id" gorm:"primaryKey"`
	RoleID int64 `json:"roleId" gorm:"uniqueIndex:idx_role_menu;index"`
	MenuID int64 `json:"menuId" gorm:"uniqueIndex:idx_role_menu;index"`
}

// RoleMenuBtn 角色-菜单-按钮关联（角色在某菜单下可用的按钮集合）。
type RoleMenuBtn struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	RoleID    int64     `json:"roleId" gorm:"uniqueIndex:idx_role_menu_btn;index"`
	MenuID    int64     `json:"menuId" gorm:"uniqueIndex:idx_role_menu_btn;index"`
	BtnID     int64     `json:"btnId" gorm:"uniqueIndex:idx_role_menu_btn;index"`
	CreatedAt time.Time `json:"createdAt"`
}

// ---------- API 与 Casbin ----------

// Api 接口表。记录系统受管 API（path+method），供 API 权限配置。
type Api struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	Path        string    `json:"path" gorm:"size:128;index"` // api 路径，如 /api/agents
	Method      string    `json:"method" gorm:"size:8"`       // GET/POST/PUT/DELETE
	Group       string    `json:"group" gorm:"size:64"`       // api 组，如 智能体管理/用户管理
	Description string    `json:"description" gorm:"size:255"`
	CreatedAt   time.Time `json:"createdAt"`
}

// TableName 指定表名 casbin_rule，与 gin-vue-admin 保持一致。
func (CasbinRule) TableName() string { return "casbin_rule" }

// CasbinRule casbin 策略存储表（v0=角色ID, v1=API路径, v2=HTTP方法）。
type CasbinRule struct {
	ID    int64  `json:"-" gorm:"primaryKey"`
	Ptype string `json:"ptype" gorm:"column:ptype;size:100;default:p"`
	V0    string `json:"v0" gorm:"column:v0;size:100"`
	V1    string `json:"v1" gorm:"column:v1;size:100"`
	V2    string `json:"v2" gorm:"column:v2;size:100"`
	V3    string `json:"v3" gorm:"column:v3;size:100"`
	V4    string `json:"v4" gorm:"column:v4;size:100"`
	V5    string `json:"v5" gorm:"column:v5;size:100"`
}
