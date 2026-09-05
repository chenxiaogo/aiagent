package store

import (
	"context"
	"strings"
	"time"

	"aiagent/internal/model"
	"aiagent/pkg/casbin"

	"gorm.io/gorm"
)

// ---------- 菜单 ----------

// ListMenus 全部菜单（含按钮定义），返回树形。
// 管理端授权界面要按菜单勾选按钮，因此这里一并填充 BtnAll（全量按钮定义）。
func (s *Store) ListMenus(ctx context.Context) []*model.Menu {
	var menus []*model.Menu
	s.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&menus)
	for _, m := range menus {
		m.BtnAll = s.GetMenuBtns(ctx, m.ID)
	}
	return buildMenuTree(menus, 0)
}

// ListAllMenusFlat 全部菜单扁平列表（含按钮）。
func (s *Store) ListAllMenusFlat(ctx context.Context) []*model.Menu {
	var menus []*model.Menu
	s.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&menus)
	for _, m := range menus {
		m.BtnAll = s.GetMenuBtns(ctx, m.ID)
	}
	return menus
}

// GetMenu 菜单详情。
func (s *Store) GetMenu(ctx context.Context, id int64) (*model.Menu, bool) {
	var m model.Menu
	if err := s.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, false
	}
	m.BtnAll = s.GetMenuBtns(ctx, id)
	return &m, true
}

// CreateMenu 创建菜单。
func (s *Store) CreateMenu(ctx context.Context, m *model.Menu) (*model.Menu, bool) {
	now := time.Now()
	m.ID = 0
	m.CreatedAt = now
	m.UpdatedAt = now
	if err := s.db.WithContext(ctx).Create(m).Error; err != nil {
		return nil, false
	}
	return m, true
}

// UpdateMenu 更新菜单。
func (s *Store) UpdateMenu(ctx context.Context, m *model.Menu) (*model.Menu, bool) {
	var old model.Menu
	if err := s.db.WithContext(ctx).First(&old, m.ID).Error; err != nil {
		return nil, false
	}
	m.CreatedAt = old.CreatedAt
	m.UpdatedAt = time.Now()
	if err := s.db.WithContext(ctx).Save(m).Error; err != nil {
		return nil, false
	}
	return m, true
}

// DeleteMenu 删除菜单（级联删除子菜单、按钮与关联）。
func (s *Store) DeleteMenu(ctx context.Context, id int64) bool {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids := s.collectMenuIDs(tx, id)
		if err := tx.Where("id IN ?", ids).Delete(&model.Menu{}).Error; err != nil {
			return err
		}
		tx.Where("menu_id IN ?", ids).Delete(&model.MenuBtn{})
		tx.Where("menu_id IN ?", ids).Delete(&model.RoleMenu{})
		tx.Where("menu_id IN ?", ids).Delete(&model.RoleMenuBtn{})
		return nil
	}) == nil
}

// collectMenuIDs 递归收集菜单及其子菜单 ID。
func (s *Store) collectMenuIDs(tx *gorm.DB, parentID int64) []int64 {
	ids := []int64{parentID}
	var children []model.Menu
	tx.Where("parent_id = ?", parentID).Find(&children)
	for _, c := range children {
		ids = append(ids, s.collectMenuIDs(tx, c.ID)...)
	}
	return ids
}

// ---------- 菜单按钮 ----------

// GetMenuBtns 获取菜单按钮定义。
func (s *Store) GetMenuBtns(ctx context.Context, menuID int64) []*model.MenuBtn {
	var btns []*model.MenuBtn
	s.db.WithContext(ctx).Where("menu_id = ?", menuID).Order("id ASC").Find(&btns)
	return btns
}

// SaveMenuBtns 保存菜单按钮（先删后插）。
func (s *Store) SaveMenuBtns(ctx context.Context, menuID int64, btns []*model.MenuBtn) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("menu_id = ?", menuID).Delete(&model.MenuBtn{}).Error; err != nil {
			return err
		}
		now := time.Now()
		for _, b := range btns {
			if b.Name == "" {
				continue
			}
			b.ID = 0
			b.MenuID = menuID
			b.CreatedAt = now
			if err := tx.Create(b).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------- 角色-菜单 ----------

// GetRoleMenuIDs 获取角色已绑定的菜单ID列表。
func (s *Store) GetRoleMenuIDs(ctx context.Context, roleID int64) []int64 {
	var rms []model.RoleMenu
	s.db.WithContext(ctx).Where("role_id = ?", roleID).Find(&rms)
	ids := make([]int64, 0, len(rms))
	for _, rm := range rms {
		ids = append(ids, rm.MenuID)
	}
	return ids
}

// SaveRoleMenus 保存角色菜单（先删后插）。
func (s *Store) SaveRoleMenus(ctx context.Context, roleID int64, menuIDs []int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&model.RoleMenu{}).Error; err != nil {
			return err
		}
		for _, mid := range menuIDs {
			if mid <= 0 {
				continue
			}
			if err := tx.Create(&model.RoleMenu{RoleID: roleID, MenuID: mid}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------- 角色-菜单-按钮 ----------

// GetRoleMenuBtnMap 获取角色各菜单下已授权的按钮：map[menuID][]btnID。
func (s *Store) GetRoleMenuBtnMap(ctx context.Context, roleID int64) map[int64][]int64 {
	var rmbs []model.RoleMenuBtn
	s.db.WithContext(ctx).Where("role_id = ?", roleID).Find(&rmbs)
	out := make(map[int64][]int64)
	for _, r := range rmbs {
		out[r.MenuID] = append(out[r.MenuID], r.BtnID)
	}
	return out
}

// SaveRoleMenuBtns 保存角色在某菜单下的按钮授权（先删后插）。
func (s *Store) SaveRoleMenuBtns(ctx context.Context, roleID, menuID int64, btnIDs []int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ? AND menu_id = ?", roleID, menuID).Delete(&model.RoleMenuBtn{}).Error; err != nil {
			return err
		}
		now := time.Now()
		for _, bid := range btnIDs {
			if bid <= 0 {
				continue
			}
			if err := tx.Create(&model.RoleMenuBtn{RoleID: roleID, MenuID: menuID, BtnID: bid, CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetRoleMenuTree 获取角色可访问的菜单树（含按钮授权），供前端动态路由/侧边栏使用。
// admin 返回全部菜单与全部按钮。
func (s *Store) GetRoleMenuTree(ctx context.Context, roleID int64, isAdmin bool) []*model.Menu {
	var allMenus []*model.Menu
	s.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&allMenus)
	if isAdmin {
		for _, m := range allMenus {
			m.Btns = s.GetMenuBtns(ctx, m.ID)
		}
		return buildMenuTree(allMenus, 0)
	}
	// 普通角色：按 role_menu 过滤 + 按钮按 role_menu_btn 过滤
	menuSet := make(map[int64]bool)
	for _, id := range s.GetRoleMenuIDs(ctx, roleID) {
		menuSet[id] = true
	}
	btnMap := s.GetRoleMenuBtnMap(ctx, roleID)
	btnSet := make(map[int64]map[int64]bool, len(btnMap))
	for mid, bids := range btnMap {
		btnSet[mid] = make(map[int64]bool, len(bids))
		for _, b := range bids {
			btnSet[mid][b] = true
		}
	}
	var menus []*model.Menu
	for _, m := range allMenus {
		if !menuSet[m.ID] {
			continue
		}
		if ids, ok := btnSet[m.ID]; ok {
			for _, b := range s.GetMenuBtns(ctx, m.ID) {
				if ids[b.ID] {
					m.Btns = append(m.Btns, b)
				}
			}
		}
		menus = append(menus, m)
	}
	return buildMenuTree(menus, 0)
}

// buildMenuTree 扁平菜单转树形。
func buildMenuTree(menus []*model.Menu, parentID int64) []*model.Menu {
	var tree []*model.Menu
	for _, m := range menus {
		if m.ParentID == parentID {
			m.Children = buildMenuTree(menus, m.ID)
			tree = append(tree, m)
		}
	}
	return tree
}

// ---------- API ----------

// ListApis 接口列表。
func (s *Store) ListApis(ctx context.Context) []*model.Api {
	var apis []*model.Api
	s.db.WithContext(ctx).Order("id ASC").Find(&apis)
	return apis
}

// CreateApi 新增接口。
func (s *Store) CreateApi(ctx context.Context, a *model.Api) (*model.Api, bool) {
	now := time.Now()
	a.ID = 0
	a.CreatedAt = now
	if err := s.db.WithContext(ctx).Create(a).Error; err != nil {
		return nil, false
	}
	return a, true
}

// UpdateApi 更新接口。
func (s *Store) UpdateApi(ctx context.Context, a *model.Api) (*model.Api, bool) {
	var old model.Api
	if err := s.db.WithContext(ctx).First(&old, a.ID).Error; err != nil {
		return nil, false
	}
	a.CreatedAt = old.CreatedAt
	if err := s.db.WithContext(ctx).Save(a).Error; err != nil {
		return nil, false
	}
	return a, true
}

// DeleteApi 删除接口（级联清理 casbin 策略）。
func (s *Store) DeleteApi(ctx context.Context, id int64) bool {
	a, ok := s.GetApi(ctx, id)
	if !ok {
		return false
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.Api{}, id).Error; err != nil {
			return err
		}
		return tx.Where("ptype = ? AND v1 = ? AND v2 = ?", "p", a.Path, a.Method).Delete(&model.CasbinRule{}).Error
	}); err != nil {
		return false
	}
	if s.Enforcer != nil {
		s.Enforcer.LoadPolicy()
	}
	return true
}

// GetApi 接口详情。
func (s *Store) GetApi(ctx context.Context, id int64) (*model.Api, bool) {
	var a model.Api
	if err := s.db.WithContext(ctx).First(&a, id).Error; err != nil {
		return nil, false
	}
	return &a, true
}

// IsManagedApi 判断某 path+method 是否为受管 API（在 sys_apis 中登记）。
func (s *Store) IsManagedApi(ctx context.Context, path, method string) bool {
	var c int64
	s.db.WithContext(ctx).Model(&model.Api{}).Where("path = ? AND method = ?", path, method).Count(&c)
	return c > 0
}

// ---------- Casbin 策略（角色-API） ----------

// SetRoleApis 全量覆盖角色的 API 权限。
func (s *Store) SetRoleApis(ctx context.Context, roleID int64, apiIDs []int64, enforcer *casbin.Enforcer) error {
	enforcer.ClearRolePolicy(roleID)
	if len(apiIDs) == 0 {
		return nil
	}
	var apis []model.Api
	s.db.WithContext(ctx).Where("id IN ?", apiIDs).Find(&apis)
	enforcer.AddPolicies(roleID, apis)
	return nil
}

// GetRoleApiIds 获取角色已授权的 API ID 列表（通过 casbin 策略反查）。
func (s *Store) GetRoleApiIds(ctx context.Context, roleID int64) []int64 {
	if s.Enforcer == nil {
		return nil
	}
	paths := make(map[string]bool)
	for _, r := range s.Enforcer.GetRolePolicies(roleID) {
		paths[r.V1+"|"+r.V2] = true
	}
	if len(paths) == 0 {
		return nil
	}
	var apis []model.Api
	s.db.WithContext(ctx).Find(&apis)
	out := make([]int64, 0, len(apis))
	for _, a := range apis {
		if paths[a.Path+"|"+strings.ToUpper(a.Method)] {
			out = append(out, a.ID)
		}
	}
	return out
}
