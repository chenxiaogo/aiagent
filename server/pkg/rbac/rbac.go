package rbac

import "aiagent/internal/model"

// PermItem 权限点定义。
type PermItem struct {
	Code        string
	Name        string
	Type        string
	Description string
}

// AllPermissions 系统全部权限点清单（用于初始化 permission 表与角色授权）。
// 权限码沿用 scheduler-platform（gocron），语义映射见 model 包权限常量注释。
var AllPermissions = []PermItem{
	{model.PermDashboardView, "查看首页看板", "menu", "查看首页 Dashboard 统计"},
	{model.PermTaskView, "查看智能体", "menu", "查看智能体列表与详情"},
	{model.PermTaskCreate, "创建智能体", "button", "新增智能体"},
	{model.PermTaskUpdate, "更新智能体", "button", "编辑智能体"},
	{model.PermTaskDelete, "删除智能体", "button", "删除智能体"},
	{model.PermTaskRun, "发布/启停智能体", "button", "发布版本、上下线智能体"},
	{model.PermExecView, "查看执行记录", "menu", "查看执行记录与详情"},
	{model.PermLogView, "查看执行日志", "button", "查看执行日志"},
	{model.PermNodeView, "查看主机", "menu", "查看运维主机列表与详情"},
	{model.PermNodeManage, "主机管理", "button", "主机增删改"},
	{model.PermHostExec, "主机命令执行", "button", "打开终端、执行命令与脚本"},
	{model.PermHostFile, "主机文件管理", "button", "上传、下载、删除、重命名主机文件"},
	{model.PermMarketView, "查看能力市场", "menu", "查看 MCP 注册表、技能库、工具库、提示词库"},
	{model.PermMarketManage, "管理能力市场", "button", "能力市场条目增删改"},
	{model.PermOpsView, "查看运行观测", "menu", "查看调用观测日志"},
	{model.PermUserManage, "用户管理", "menu", "用户增删改查与分配角色"},
	{model.PermRoleManage, "角色管理", "menu", "角色增删改查与授权"},
	{model.PermNotifyView, "查看通知配置", "menu", "查看全局通知配置"},
	{model.PermNotifyManage, "管理通知配置", "button", "保存通知配置与发送测试"},
	{model.PermSystemAdmin, "系统管理员", "role", "系统管理员，拥有全部权限"},
}
