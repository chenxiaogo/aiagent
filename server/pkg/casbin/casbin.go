package casbin

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"aiagent/internal/model"

	"gorm.io/gorm"
)

// Enforcer 基于数据库 casbin_rule 表的策略执行器。
// 策略格式：p = sub(角色ID), obj(API路径), act(HTTP方法)。
// 匹配规则参考 gin-vue-admin：r.sub == p.sub && keyMatch2(r.obj,p.obj) && r.act == p.act。
type Enforcer struct {
	db  *gorm.DB
	mu  sync.RWMutex
	// policies 内存缓存：roleID -> []policy{path, method}
	policies map[string][]policy
}

type policy struct {
	path   string
	method string
}

// New 创建 Enforcer 并加载全部策略到内存缓存。
func New(db *gorm.DB) *Enforcer {
	e := &Enforcer{
		db:       db,
		policies: make(map[string][]policy),
	}
	e.LoadPolicy()
	return e
}

// LoadPolicy 从数据库重新加载全部策略。
func (e *Enforcer) LoadPolicy() {
	var rules []model.CasbinRule
	e.db.Where("ptype = ?", "p").Find(&rules)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.policies = make(map[string][]policy, len(rules))
	for _, r := range rules {
		if r.V1 == "" {
			continue
		}
		e.policies[r.V0] = append(e.policies[r.V0], policy{path: r.V1, method: strings.ToUpper(r.V2)})
	}
}

// AddPolicies 为角色新增策略（幂等去重）。
func (e *Enforcer) AddPolicies(roleID int64, apis []model.Api) {
	sub := strconv.FormatInt(roleID, 10)
	for _, a := range apis {
		if a.Path == "" {
			continue
		}
		e.db.Create(&model.CasbinRule{Ptype: "p", V0: sub, V1: a.Path, V2: strings.ToUpper(a.Method)})
	}
	e.LoadPolicy()
}

// AddPolicy 新增单条策略。
func (e *Enforcer) AddPolicy(roleID int64, path, method string) {
	sub := strconv.FormatInt(roleID, 10)
	e.db.Create(&model.CasbinRule{Ptype: "p", V0: sub, V1: path, V2: strings.ToUpper(method)})
	e.LoadPolicy()
}

// RemovePolicy 删除单条策略。
func (e *Enforcer) RemovePolicy(roleID int64, path, method string) {
	sub := strconv.FormatInt(roleID, 10)
	e.db.Where("ptype = ? AND v0 = ? AND v1 = ? AND v2 = ?", "p", sub, path, strings.ToUpper(method)).
		Delete(&model.CasbinRule{})
	e.LoadPolicy()
}

// ClearRolePolicy 清空角色全部策略。
func (e *Enforcer) ClearRolePolicy(roleID int64) {
	sub := strconv.FormatInt(roleID, 10)
	e.db.Where("ptype = ? AND v0 = ?", "p", sub).Delete(&model.CasbinRule{})
	e.LoadPolicy()
}

// GetRolePolicies 查询角色策略列表。
func (e *Enforcer) GetRolePolicies(roleID int64) []model.CasbinRule {
	var rules []model.CasbinRule
	sub := strconv.FormatInt(roleID, 10)
	e.db.Where("ptype = ? AND v0 = ?", "p", sub).Find(&rules)
	return rules
}

// Enforce 校验角色是否有权访问指定 path+method。admin 角色恒放行。
func (e *Enforcer) Enforce(roleID int64, isAdmin bool, path, method string) bool {
	if isAdmin {
		return true
	}
	sub := strconv.FormatInt(roleID, 10)
	method = strings.ToUpper(method)

	e.mu.RLock()
	pols := e.policies[sub]
	e.mu.RUnlock()
	for _, p := range pols {
		if p.method == method && keyMatch2(path, p.path) {
			return true
		}
	}
	return false
}

// keyMatch2 判断是否匹配，支持 :param 与 * 通配。
// 参考 casbin keyMatch2 实现。
var keyMatch2RegexCache = map[string]*regexp.Regexp{}
var km2mu sync.Mutex

func keyMatch2(key1, key2 string) bool {
	if key2 == "*" {
		return true
	}
	if strings.HasSuffix(key2, "/*") {
		prefix := strings.TrimSuffix(key2, "*")
		return strings.HasPrefix(key1, prefix)
	}
	km2mu.Lock()
	re, ok := keyMatch2RegexCache[key2]
	if !ok {
		pattern := regexp.MustCompile(`:[^/]+`).ReplaceAllString(key2, `[^/]+`)
		pattern = "^" + pattern + "$"
		re = regexp.MustCompile(pattern)
		keyMatch2RegexCache[key2] = re
	}
	km2mu.Unlock()
	return re.MatchString(key1)
}
