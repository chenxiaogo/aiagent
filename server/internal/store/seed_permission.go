package store

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"aiagent/internal/model"
	"aiagent/pkg/ilog"
)

// InitPermissionData 初始化权限体系：权限点 → 内置角色 → 菜单树 → 受管 API → 内置角色菜单绑定。
// 全程幂等，可随进程启动反复调用。
func (s *Store) InitPermissionData(ctx context.Context) {
	if err := s.InitRBAC(ctx); err != nil {
		ilog.Errorf("init rbac failed: %v", err)
		return
	}

	var menuCount int64
	s.db.WithContext(ctx).Model(&model.Menu{}).Count(&menuCount)
	if menuCount == 0 {
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return s.seedMenus(tx)
		}); err != nil {
			ilog.Errorf("seed menus failed: %v", err)
		}
	} else {
		// 既有库：幂等补齐后加的菜单，避免升级后缺入口
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return s.ensureMissingMenus(tx)
		}); err != nil {
			ilog.Errorf("ensure menus failed: %v", err)
		}
	}

	s.seedApis(ctx)

	// 运维主机不进平台菜单：清掉早期版本建的记录（幂等），
	// 必须在重建角色菜单绑定之前，否则内置角色仍会绑到已删除的菜单上。
	s.removeHostMenus(ctx)
	// 「模型路由」菜单迁入运行观测目录（幂等，老库升级自动归位）
	s.migrateModelCatalogMenu(ctx)

	// 菜单确定后再重建内置角色的菜单/按钮绑定，保证与最新菜单结构一致
	s.RebuildBuiltinRoleMenus(ctx)
	// 内置角色同样要发 API 策略，否则受管接口会被 CasbinAuth 拦下（策略表默认空）
	s.RebuildBuiltinRoleApis(ctx)

	// 把现有 admin 账号挂到内置 admin 角色，避免出现「有权限点但无角色」的空档
	s.bindLegacyAdminRole(ctx)
}

// RebuildBuiltinRoleApis 清空并重建内置角色（admin/operator/viewer）的受管接口授权。
//
// 必要性：CasbinAuth 只对登记在 sys_apis 里的接口做校验，而新建角色的策略表是空的。
// 不给内置角色下发策略，operator 明明有 node:manage 权限也调不动 POST /api/hosts。
//
// 口径：admin 全部；operator 排除系统管理类；viewer 在 operator 基础上只保留 GET。
func (s *Store) RebuildBuiltinRoleApis(ctx context.Context) {
	if s.Enforcer == nil {
		return
	}
	var roles []model.Role
	if err := s.db.WithContext(ctx).Where("built_in = ?", true).Find(&roles).Error; err != nil || len(roles) == 0 {
		return
	}
	var apis []model.Api
	s.db.WithContext(ctx).Find(&apis)
	if len(apis) == 0 {
		return
	}
	// 权限体系自身的接口只给 admin，避免普通角色改动授权
	adminOnlyGroups := map[string]bool{
		"用户管理": true, "角色管理": true, "菜单管理": true, "接口管理": true,
	}
	for _, role := range roles {
		allowed := make([]model.Api, 0, len(apis))
		for _, a := range apis {
			switch role.Code {
			case "admin":
				allowed = append(allowed, a)
			case "operator":
				if !adminOnlyGroups[a.Group] {
					allowed = append(allowed, a)
				}
			case "viewer":
				if !adminOnlyGroups[a.Group] && strings.EqualFold(a.Method, "GET") {
					allowed = append(allowed, a)
				}
			}
		}
		s.Enforcer.ClearRolePolicy(role.ID)
		if len(allowed) > 0 {
			s.Enforcer.AddPolicies(role.ID, allowed)
		}
	}
	ilog.Infof("rebuilt builtin role api policies for %d roles", len(roles))
}

// seedMenus 写入 aiagent 默认菜单树与按钮。
// 结构：智能体（目录）｜运维主机（目录）｜能力市场（目录）｜运行观测（目录）｜系统设置（目录）
func (s *Store) seedMenus(tx *gorm.DB) error {
	now := time.Now()

	// ---------- 智能体 ----------
	agentDir := &model.Menu{Name: "Agents", Path: "/agents", Title: "智能体", Icon: "MagicStick", Sort: 1, Type: "dir", PermCode: model.PermTaskView, CreatedAt: now}
	if err := tx.Create(agentDir).Error; err != nil {
		return err
	}
	agentList := &model.Menu{Name: "AgentList", Path: "/agents", Component: "view/agent/AgentList", Title: "智能体管理", Icon: "MagicStick", Sort: 1, Type: "menu", PermCode: model.PermTaskView, ParentID: agentDir.ID, CreatedAt: now}
	for _, m := range []*model.Menu{agentList} {
		if err := tx.Create(m).Error; err != nil {
			return err
		}
	}

	// ---------- 能力市场 ----------
	// 注意：运维主机不建平台菜单。主机在运维型 Agent 工作台的主机面板里维护，
	// 平台侧边栏不单列入口（项目原设计）。其权限点仍在角色授权页可勾选。
	marketDir := &model.Menu{Name: "Market", Path: "/market", Title: "能力市场", Icon: "Shop", Sort: 2, Type: "dir", PermCode: model.PermMarketView, CreatedAt: now}
	if err := tx.Create(marketDir).Error; err != nil {
		return err
	}
	marketMenus := []*model.Menu{
		{Name: "MarketMCP", Path: "/market/mcp", Component: "view/market/MCPRegistry", Title: "MCP 注册表", Icon: "Connection", Sort: 1, Type: "menu", PermCode: model.PermMarketView},
		{Name: "MarketSkills", Path: "/market/skills", Component: "view/market/SkillLibrary", Title: "技能库", Icon: "Collection", Sort: 2, Type: "menu", PermCode: model.PermMarketView},
		{Name: "MarketTools", Path: "/market/tools", Component: "view/market/ToolLibrary", Title: "工具库", Icon: "Box", Sort: 3, Type: "menu", PermCode: model.PermMarketView},
		{Name: "MarketPrompts", Path: "/market/prompts", Component: "view/market/PromptLibrary", Title: "提示词库", Icon: "ChatDotSquare", Sort: 4, Type: "menu", PermCode: model.PermMarketView},
		// 模型目录（单价）与模型路由已迁至「运行观测」目录（/ops/models），见 ops 目录定义。
		{Name: "MarketKnowledge", Path: "/market/knowledge", Component: "view/knowledge/KnowledgeBaseManager", Title: "知识库", Icon: "Files", Sort: 5, Type: "menu", PermCode: model.PermMarketView},
	}
	for _, m := range marketMenus {
		m.ParentID = marketDir.ID
		m.CreatedAt = now
		if err := tx.Create(m).Error; err != nil {
			return err
		}
	}

	// ---------- 运行观测 ----------
	opsDir := &model.Menu{Name: "Ops", Path: "/ops", Title: "运行观测", Icon: "DataLine", Sort: 3, Type: "dir", PermCode: model.PermOpsView, CreatedAt: now}
	if err := tx.Create(opsDir).Error; err != nil {
		return err
	}
	opsLogs := &model.Menu{Name: "OpsCallLogs", Path: "/ops/call-logs", Component: "view/ops/CallLogs", Title: "调用观测", Icon: "DataLine", Sort: 1, Type: "menu", PermCode: model.PermOpsView, ParentID: opsDir.ID, CreatedAt: now}
	if err := tx.Create(opsLogs).Error; err != nil {
		return err
	}
	// 模型路由：从能力市场迁入运行观测，与「调用观测」并列（页面仍是 Tab 结构：模型目录 / 模型路由）。
	// 不要指向 view/settings/ModelConfig —— 那是「系统设置 → 大模型配置」，
	// 管的是连接参数（API Key / Base URL），与这里两回事。
	// Name 沿用 MarketModels 作为稳定锚点，既有库由 migrateModelCatalogMenu 幂等迁移。
	opsModels := &model.Menu{Name: "MarketModels", Path: "/ops/models", Component: "view/market/ModelCatalog", Title: "模型路由", Icon: "Cpu", Sort: 2, Type: "menu", PermCode: model.PermOpsView, ParentID: opsDir.ID, CreatedAt: now}
	if err := tx.Create(opsModels).Error; err != nil {
		return err
	}

	// ---------- 系统设置 ----------
	// 目录挂 PermRoleManage：仅具备角色管理权限的角色（默认仅 admin）可见。
	settingsDir := &model.Menu{Name: "Settings", Path: "/settings", Title: "系统设置", Icon: "Setting", Sort: 4, Type: "dir", PermCode: model.PermRoleManage, CreatedAt: now}
	if err := tx.Create(settingsDir).Error; err != nil {
		return err
	}
	settingsMenus := []*model.Menu{
		{Name: "SettingsModels", Path: "/settings/models", Component: "view/settings/ModelConfig", Title: "大模型配置", Icon: "Cpu", Sort: 1, Type: "menu", PermCode: model.PermMarketManage},
		{Name: "Users", Path: "/settings/users", Component: "view/user/UserList", Title: "用户管理", Icon: "User", Sort: 2, Type: "menu", PermCode: model.PermUserManage},
		{Name: "Roles", Path: "/settings/roles", Component: "view/role/RoleList", Title: "角色管理", Icon: "Key", Sort: 3, Type: "menu", PermCode: model.PermRoleManage},
		{Name: "Menus", Path: "/settings/menus", Component: "view/system/MenuManage", Title: "菜单管理", Icon: "Grid", Sort: 4, Type: "menu", PermCode: model.PermRoleManage},
		{Name: "Apis", Path: "/settings/apis", Component: "view/system/ApiManage", Title: "接口管理", Icon: "Link", Sort: 5, Type: "menu", PermCode: model.PermRoleManage},
		{Name: "SettingsReindex", Path: "/settings/reindex", Component: "view/settings/Reindex", Title: "索引维护", Icon: "RefreshRight", Sort: 6, Type: "menu", PermCode: model.PermRoleManage},
	}
	for _, m := range settingsMenus {
		m.ParentID = settingsDir.ID
		m.CreatedAt = now
		if err := tx.Create(m).Error; err != nil {
			return err
		}
	}

	// ---------- 菜单按钮 ----------
	btnGroups := []struct {
		menuID int64
		btns   []*model.MenuBtn
	}{
		{agentList.ID, []*model.MenuBtn{
			{Name: "add", Desc: "新增智能体", PermCode: model.PermTaskCreate},
			{Name: "edit", Desc: "编辑智能体", PermCode: model.PermTaskUpdate},
			{Name: "delete", Desc: "删除智能体", PermCode: model.PermTaskDelete},
			{Name: "run", Desc: "发布智能体", PermCode: model.PermTaskRun},
		}},
		{marketMenus[0].ID, []*model.MenuBtn{
			{Name: "add", Desc: "新增", PermCode: model.PermMarketManage},
			{Name: "edit", Desc: "编辑", PermCode: model.PermMarketManage},
			{Name: "delete", Desc: "删除", PermCode: model.PermMarketManage},
		}},
		{marketMenus[1].ID, []*model.MenuBtn{
			{Name: "add", Desc: "新增", PermCode: model.PermMarketManage},
			{Name: "edit", Desc: "编辑", PermCode: model.PermMarketManage},
			{Name: "delete", Desc: "删除", PermCode: model.PermMarketManage},
		}},
		{marketMenus[2].ID, []*model.MenuBtn{
			{Name: "add", Desc: "新增", PermCode: model.PermMarketManage},
			{Name: "edit", Desc: "编辑", PermCode: model.PermMarketManage},
			{Name: "delete", Desc: "删除", PermCode: model.PermMarketManage},
		}},
		{marketMenus[3].ID, []*model.MenuBtn{
			{Name: "add", Desc: "新增", PermCode: model.PermMarketManage},
			{Name: "edit", Desc: "编辑", PermCode: model.PermMarketManage},
			{Name: "delete", Desc: "删除", PermCode: model.PermMarketManage},
		}},
		{settingsMenus[1].ID, []*model.MenuBtn{
			{Name: "add", Desc: "新增用户", PermCode: model.PermUserManage},
			{Name: "edit", Desc: "编辑用户", PermCode: model.PermUserManage},
			{Name: "delete", Desc: "删除用户", PermCode: model.PermUserManage},
			{Name: "reset", Desc: "重置密码", PermCode: model.PermUserManage},
		}},
		{settingsMenus[2].ID, []*model.MenuBtn{
			{Name: "add", Desc: "新增角色", PermCode: model.PermRoleManage},
			{Name: "edit", Desc: "编辑角色", PermCode: model.PermRoleManage},
			{Name: "delete", Desc: "删除角色", PermCode: model.PermRoleManage},
			{Name: "auth", Desc: "分配权限", PermCode: model.PermRoleManage},
		}},
	}
	for _, g := range btnGroups {
		for _, b := range g.btns {
			b.MenuID = g.menuID
			b.CreatedAt = now
			if err := tx.Create(b).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// ensureMissingMenus 既有库补齐：按 name 锚点补齐后加的菜单（本次为运维主机与系统设置下的管理页）。
func (s *Store) ensureMissingMenus(tx *gorm.DB) error {
	now := time.Now()
	getByName := func(name string) *model.Menu {
		var m model.Menu
		if err := tx.Where("name = ?", name).First(&m).Error; err != nil {
			return nil
		}
		return &m
	}
	ensure := func(dirName, dirTitle, dirIcon string, dirSort int, dirPerm string, items []*model.Menu) error {
		dir := getByName(dirName)
		if dir == nil {
			dir = &model.Menu{Name: dirName, Path: "/" + lowerFirst(dirName), Title: dirTitle, Icon: dirIcon, Sort: dirSort, Type: "dir", PermCode: dirPerm, CreatedAt: now}
			if err := tx.Create(dir).Error; err != nil {
				return err
			}
		}
		for i, m := range items {
			if getByName(m.Name) != nil {
				continue
			}
			m.ParentID = dir.ID
			m.Sort = i + 1
			m.CreatedAt = now
			if err := tx.Create(m).Error; err != nil {
				return err
			}
		}
		return nil
	}

	// 运维主机不建平台菜单，见 seedMenus 中的说明
	return ensure("Settings", "系统设置", "Setting", 4, model.PermRoleManage, []*model.Menu{
		{Name: "Users", Path: "/settings/users", Component: "view/user/UserList", Title: "用户管理", Icon: "User", Type: "menu", PermCode: model.PermUserManage},
		{Name: "Roles", Path: "/settings/roles", Component: "view/role/RoleList", Title: "角色管理", Icon: "Key", Type: "menu", PermCode: model.PermRoleManage},
		{Name: "Menus", Path: "/settings/menus", Component: "view/system/MenuManage", Title: "菜单管理", Icon: "Grid", Type: "menu", PermCode: model.PermRoleManage},
		{Name: "Apis", Path: "/settings/apis", Component: "view/system/ApiManage", Title: "接口管理", Icon: "Link", Type: "menu", PermCode: model.PermRoleManage},
		{Name: "SettingsReindex", Path: "/settings/reindex", Component: "view/settings/Reindex", Title: "索引维护", Icon: "RefreshRight", Type: "menu", PermCode: model.PermRoleManage},
	})
}

// seedApis 登记受管 API。只有登记在案的 path+method 才会被 CasbinAuth 拦截，
// 未登记的接口保持开放，避免误伤既有功能。
func (s *Store) seedApis(ctx context.Context) {
	items := []struct {
		path, method, group, desc string
	}{
		// 用户管理
		{"/api/users", "GET", "用户管理", "用户列表"},
		{"/api/users", "POST", "用户管理", "创建用户"},
		{"/api/users/:id", "PUT", "用户管理", "更新用户"},
		{"/api/users/:id/password", "PUT", "用户管理", "重置密码"},
		{"/api/users/:id", "DELETE", "用户管理", "删除用户"},
		// 角色管理
		{"/api/roles", "GET", "角色管理", "角色列表"},
		{"/api/roles", "POST", "角色管理", "创建角色"},
		{"/api/roles/:id", "PUT", "角色管理", "更新角色"},
		{"/api/roles/:id/perms", "PUT", "角色管理", "设置角色权限点"},
		{"/api/roles/:id/menus", "PUT", "角色管理", "设置角色菜单"},
		{"/api/roles/:id/apis", "PUT", "角色管理", "设置角色接口"},
		{"/api/roles/:id", "DELETE", "角色管理", "删除角色"},
		// 菜单管理
		{"/api/menus", "GET", "菜单管理", "菜单树"},
		{"/api/menus", "POST", "菜单管理", "新增菜单"},
		{"/api/menus/:id", "PUT", "菜单管理", "更新菜单"},
		{"/api/menus/:id/btns", "PUT", "菜单管理", "保存菜单按钮"},
		{"/api/menus/:id", "DELETE", "菜单管理", "删除菜单"},
		// 接口管理
		{"/api/apis", "GET", "接口管理", "接口列表"},
		{"/api/apis", "POST", "接口管理", "新增接口"},
		{"/api/apis/:id", "PUT", "接口管理", "更新接口"},
		{"/api/apis/:id", "DELETE", "接口管理", "删除接口"},
		// 智能体
		{"/api/agents", "POST", "智能体管理", "创建智能体"},
		{"/api/agents/:id", "PUT", "智能体管理", "更新智能体"},
		{"/api/agents/:id", "DELETE", "智能体管理", "删除智能体"},
		// 运维主机
		{"/api/hosts", "POST", "运维主机", "创建主机"},
		{"/api/hosts/:id", "PUT", "运维主机", "更新主机"},
		{"/api/hosts/:id", "DELETE", "运维主机", "删除主机"},
		{"/api/hosts/groups", "POST", "运维主机", "创建主机组"},
		{"/api/hosts/groups/:id", "PUT", "运维主机", "更新主机组"},
		{"/api/hosts/groups/:id", "DELETE", "运维主机", "删除主机组"},
	}
	now := time.Now()
	for _, it := range items {
		var c int64
		s.db.WithContext(ctx).Model(&model.Api{}).Where("path = ? AND method = ?", it.path, it.method).Count(&c)
		if c > 0 {
			continue
		}
		s.db.WithContext(ctx).Create(&model.Api{
			Path: it.path, Method: it.method, Group: it.group, Description: it.desc, CreatedAt: now,
		})
	}
}

// RebuildBuiltinRoleMenus 清空并重建内置角色（admin/operator/viewer）的菜单与按钮绑定。
// 基于提交后的最新菜单/按钮数据，先清后插保证幂等无重复。
func (s *Store) RebuildBuiltinRoleMenus(ctx context.Context) {
	db := s.db.WithContext(ctx)
	var roles []model.Role
	if err := db.Where("built_in = ?", true).Find(&roles).Error; err != nil || len(roles) == 0 {
		return
	}
	roleIDs := make([]int64, 0, len(roles))
	for _, r := range roles {
		roleIDs = append(roleIDs, r.ID)
	}
	db.Where("role_id IN ?", roleIDs).Delete(&model.RoleMenu{})
	db.Where("role_id IN ?", roleIDs).Delete(&model.RoleMenuBtn{})

	var menus []model.Menu
	db.Find(&menus)
	now := time.Now()

	bindRole := func(roleID int64, permSet map[string]bool) {
		for _, m := range menus {
			// 无 permCode 的菜单所有人可见；否则需在角色授权集合内
			if m.PermCode != "" && !permSet[m.PermCode] {
				continue
			}
			db.Create(&model.RoleMenu{RoleID: roleID, MenuID: m.ID})
			for _, b := range s.GetMenuBtns(ctx, m.ID) {
				if b.PermCode == "" || permSet[b.PermCode] {
					db.Create(&model.RoleMenuBtn{RoleID: roleID, MenuID: m.ID, BtnID: b.ID, CreatedAt: now})
				}
			}
		}
	}

	for _, role := range roles {
		switch role.Code {
		case "admin":
			for _, m := range menus {
				db.Create(&model.RoleMenu{RoleID: role.ID, MenuID: m.ID})
				for _, b := range s.GetMenuBtns(ctx, m.ID) {
					db.Create(&model.RoleMenuBtn{RoleID: role.ID, MenuID: m.ID, BtnID: b.ID, CreatedAt: now})
				}
			}
		case "operator":
			bindRole(role.ID, map[string]bool{
				model.PermTaskView: true, model.PermTaskCreate: true, model.PermTaskUpdate: true,
				model.PermTaskRun: true, model.PermExecView: true, model.PermLogView: true,
				model.PermNodeView: true, model.PermNodeManage: true,
				model.PermHostExec: true, model.PermHostFile: true,
				model.PermMarketView: true, model.PermOpsView: true,
			})
		case "viewer":
			bindRole(role.ID, map[string]bool{
				model.PermTaskView: true, model.PermExecView: true, model.PermLogView: true,
				model.PermNodeView: true, model.PermMarketView: true, model.PermOpsView: true,
			})
		}
	}
}

// bindLegacyAdminRole 把既有的 admin 账号挂到内置 admin 角色。
// 历史库里 ensureAdmin 只置了 is_admin=true、role_id=0，
// 不补这一下会出现「有权限点但角色为空」，角色管理页显示不出角色名。
func (s *Store) bindLegacyAdminRole(ctx context.Context) {
	role, ok := s.GetRoleByCode(ctx, "admin")
	if !ok {
		return
	}
	s.db.WithContext(ctx).Model(&model.User{}).
		Where("is_admin = ? AND role_id = ?", true, 0).
		Update("role_id", role.ID)
}

// migrateModelCatalogMenu 把「模型目录与路由」菜单迁入运行观测目录并更名「模型路由」（幂等）。
//
// 历史：早期它挂在能力市场目录（/market/models，名「模型目录与路由」），还曾指向
// view/settings/ModelConfig（大模型配置页），与「系统设置 → 大模型配置」完全重复，
// 于是点「模型目录」进到的是配 API Key 的页面。真正的页面是 view/market/ModelCatalog
// （Tab 结构：模型目录 / 模型路由）。
// 本次调整：页面归入「运行观测」，与「调用观测」并列，名称改为「模型路由」。
// Name 保留 MarketModels 作锚点，每次启动校正，保证新老库结构一致。
func (s *Store) migrateModelCatalogMenu(ctx context.Context) {
	db := s.db.WithContext(ctx)
	var opsDir model.Menu
	if err := db.Where("name = ?", "Ops").First(&opsDir).Error; err != nil {
		return
	}
	res := db.Model(&model.Menu{}).
		Where("name = ?", "MarketModels").
		Updates(map[string]interface{}{
			"parent_id":  opsDir.ID,
			"path":       "/ops/models",
			"title":      "模型路由",
			"icon":       "Cpu",
			"component":  "view/market/ModelCatalog",
			"perm_code":  model.PermOpsView,
			"sort":       2,
			"updated_at": time.Now(),
		})
	if res.Error == nil && res.RowsAffected > 0 {
		ilog.Infof("menu migration: moved model catalog to ops (%d rows)", res.RowsAffected)
	}
}

// removeHostMenus 移除运维主机的平台菜单（幂等）。
//
// 主机在运维型 Agent 工作台的主机面板里维护，平台侧边栏不单列入口。
// 早期版本把它建成了菜单目录，这里把既有库的记录一并清掉，
// 连带清理按钮定义与角色绑定，避免留下孤儿数据和失效的侧边栏入口。
// 权限点（node:* / host:*）保留，角色授权页仍可勾选，主机接口校验照常生效。
func (s *Store) removeHostMenus(ctx context.Context) {
	db := s.db.WithContext(ctx)
	var targets []model.Menu
	if err := db.Where("name IN ?", []string{"Hosts", "HostList"}).Find(&targets).Error; err != nil || len(targets) == 0 {
		return
	}
	// 目录下面可能还挂着子菜单，递归收集后一并删除
	seen := make(map[int64]bool, len(targets))
	ids := make([]int64, 0, len(targets))
	for _, m := range targets {
		for _, id := range s.collectMenuIDs(db, m.ID) {
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	db.Where("menu_id IN ?", ids).Delete(&model.MenuBtn{})
	db.Where("menu_id IN ?", ids).Delete(&model.RoleMenu{})
	db.Where("menu_id IN ?", ids).Delete(&model.RoleMenuBtn{})
	db.Where("id IN ?", ids).Delete(&model.Menu{})
	ilog.Infof("menu migration: removed %d host menus (moved to agent workspace)", len(ids))
}

// lowerFirst 首字母小写，用于 ensureMissingMenus 推导目录路径。
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}
