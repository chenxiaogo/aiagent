package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentscope "aiagent/internal/agent"
	"aiagent/internal/approval"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/internal/toolkit"
	"aiagent/pkg/sshx"
)

// opsBuiltinTools 返回运维类内置工具的 handler 映射。
// 工具定义从 tool_libraries 表读取，执行逻辑在此处实现。
func (r *AgentRuntime) opsBuiltinTools(ctx context.Context, st *store.Store) map[string]toolkit.Handler {
	if st == nil {
		return nil
	}
	m := map[string]toolkit.Handler{
		// 列出可操作的主机列表
		"list_hosts": func(ctx context.Context, args map[string]any) (any, error) {
			// 仅确认调用链带有可信作用域，可见范围由 scopedHosts 判定
			_, err := agentscope.RequireScope(ctx)
			if err != nil {
				return nil, err
			}
			hosts, err := scopedHosts(ctx, st)
			if err != nil {
				return nil, err
			}
			result := make([]map[string]any, 0, len(hosts))
			for _, h := range hosts {
				result = append(result, map[string]any{
					"id":       h.ID,
					"name":     h.Name,
					"hostname": h.Hostname,
					"port":     h.Port,
					"username": h.Username,
					"os":       h.OS,
					"status":   h.Status,
					"groupId":  h.GroupID,
					"tags":     h.Tags,
				})
			}
			return result, nil
		},

		// 在指定主机执行命令
		"exec_command": func(ctx context.Context, args map[string]any) (any, error) {
			scope, err := agentscope.RequireScope(ctx)
			if err != nil {
				return nil, err
			}
			hostID := getInt64(args, "host_id", 0)
			command := getString(args, "command", "")
			if hostID <= 0 {
				return nil, fmt.Errorf("host_id 必填")
			}
			if strings.TrimSpace(command) == "" {
				return nil, fmt.Errorf("command 必填")
			}
			// 红线兜底：无论从聊天还是 MCP 链路进来，灾难性命令一律拒绝执行
			if assessment := approval.AssessToolCall("exec_command", args); assessment.Block {
				return nil, fmt.Errorf("%s", assessment.Reason)
			}
			// 校验主机是否在 Agent 授权范围内
			host, err := st.GetHost(ctx, hostID)
			if err != nil {
				return nil, fmt.Errorf("主机不存在: %w", err)
			}
			if err := checkHostAccess(ctx, st, host); err != nil {
				return nil, err
			}
			if hostUnreachable(host) {
				return nil, fmt.Errorf("主机状态为「%s」，请先确认主机可达后再试", host.Status)
			}

			// 创建命令记录（审计）
			record := &model.HostCommandRecord{
				HostID:   host.ID,
				HostName: host.Name,
				AgentID:  scope.AgentID,
				UserID:   scope.UserID,
				Command:  command,
				Status:   "running",
			}
			st.CreateHostCommandRecord(ctx, record)

			// SSH 连接并执行
			cfg := sshx.HostConfig{
				Hostname:   host.Hostname,
				Port:       host.Port,
				Username:   host.Username,
				Password:   host.Password,
				PrivateKey: host.PrivateKey,
				Passphrase: host.Passphrase,
				Timeout:    30 * time.Second,
			}
			client, err := sshx.Dial(cfg)
			if err != nil {
				st.FinishHostCommandRecord(ctx, record.ID, -1, "", err.Error(), 0, "failed")
				return nil, fmt.Errorf("连接主机失败: %w", err)
			}
			defer client.Close()

			// 命令超时：默认 60s
			execCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()

			result, execErr := client.Exec(execCtx, command)
			status := "success"
			if execErr != nil {
				status = "failed"
			}
			st.FinishHostCommandRecord(ctx, record.ID, result.ExitCode, result.Stdout, result.Stderr, result.DurationMs, status)

			return map[string]any{
				"host_id":    host.ID,
				"host_name":  host.Name,
				"command":    command,
				"exit_code":  result.ExitCode,
				"stdout":     truncate(result.Stdout, 8000),
				"stderr":     truncate(result.Stderr, 2000),
				"duration_ms": result.DurationMs,
				"success":    execErr == nil,
			}, nil
		},

		// 列出远程目录
		"list_dir": func(ctx context.Context, args map[string]any) (any, error) {
			// 仅确认调用链带有可信作用域，具体授权由 checkHostAccess 判定
			_, err := agentscope.RequireScope(ctx)
			if err != nil {
				return nil, err
			}
			hostID := getInt64(args, "host_id", 0)
			path := getString(args, "path", "/")
			if hostID <= 0 {
				return nil, fmt.Errorf("host_id 必填")
			}
			host, err := st.GetHost(ctx, hostID)
			if err != nil {
				return nil, fmt.Errorf("主机不存在: %w", err)
			}
			if err := checkHostAccess(ctx, st, host); err != nil {
				return nil, err
			}
			cfg := sshx.HostConfig{
				Hostname:   host.Hostname,
				Port:       host.Port,
				Username:   host.Username,
				Password:   host.Password,
				PrivateKey: host.PrivateKey,
				Passphrase: host.Passphrase,
				Timeout:    15 * time.Second,
			}
			client, err := sshx.Dial(cfg)
			if err != nil {
				return nil, fmt.Errorf("连接主机失败: %w", err)
			}
			defer client.Close()

			entries, err := client.ListDir(ctx, path)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"host_id": host.ID,
				"path":    path,
				"entries": entries,
			}, nil
		},

		// 读取远程文件
		"read_file": func(ctx context.Context, args map[string]any) (any, error) {
			// 仅确认调用链带有可信作用域，具体授权由 checkHostAccess 判定
			_, err := agentscope.RequireScope(ctx)
			if err != nil {
				return nil, err
			}
			hostID := getInt64(args, "host_id", 0)
			path := getString(args, "path", "")
			if hostID <= 0 || path == "" {
				return nil, fmt.Errorf("host_id 和 path 必填")
			}
			host, err := st.GetHost(ctx, hostID)
			if err != nil {
				return nil, fmt.Errorf("主机不存在: %w", err)
			}
			if err := checkHostAccess(ctx, st, host); err != nil {
				return nil, err
			}
			cfg := sshx.HostConfig{
				Hostname:   host.Hostname,
				Port:       host.Port,
				Username:   host.Username,
				Password:   host.Password,
				PrivateKey: host.PrivateKey,
				Passphrase: host.Passphrase,
				Timeout:    15 * time.Second,
			}
			client, err := sshx.Dial(cfg)
			if err != nil {
				return nil, fmt.Errorf("连接主机失败: %w", err)
			}
			defer client.Close()

			content, err := client.ReadFile(ctx, path, 0)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"host_id": host.ID,
				"path":    path,
				"content": truncate(content, 32000),
			}, nil
		},

		// 写入远程文件
		"write_file": func(ctx context.Context, args map[string]any) (any, error) {
			// 仅确认调用链带有可信作用域，具体授权由 checkHostAccess 判定
			_, err := agentscope.RequireScope(ctx)
			if err != nil {
				return nil, err
			}
			hostID := getInt64(args, "host_id", 0)
			path := getString(args, "path", "")
			content := getString(args, "content", "")
			backup := getBool(args, "backup", true)
			if hostID <= 0 || path == "" {
				return nil, fmt.Errorf("host_id 和 path 必填")
			}
			// 红线兜底：禁止写入系统关键路径
			if assessment := approval.AssessToolCall("write_file", args); assessment.Block {
				return nil, fmt.Errorf("%s", assessment.Reason)
			}
			host, err := st.GetHost(ctx, hostID)
			if err != nil {
				return nil, fmt.Errorf("主机不存在: %w", err)
			}
			if err := checkHostAccess(ctx, st, host); err != nil {
				return nil, err
			}
			cfg := sshx.HostConfig{
				Hostname:   host.Hostname,
				Port:       host.Port,
				Username:   host.Username,
				Password:   host.Password,
				PrivateKey: host.PrivateKey,
				Passphrase: host.Passphrase,
				Timeout:    30 * time.Second,
			}
			client, err := sshx.Dial(cfg)
			if err != nil {
				return nil, fmt.Errorf("连接主机失败: %w", err)
			}
			defer client.Close()

			if err := client.WriteFile(ctx, path, content, backup); err != nil {
				return nil, fmt.Errorf("写入文件失败: %w", err)
			}
			return map[string]any{
				"host_id": host.ID,
				"path":    path,
				"backup":  backup,
				"success": true,
			}, nil
		},
	}
	for k, v := range r.opsHostTools(ctx, st) {
		m[k] = v
	}
	return m
}

// checkHostAccess 校验当前会话能否操作这台主机。
//
// 授权来源按优先级：
//  1. 会话作用域 —— 运维工作台按主机 / 主机组开的会话，用户选中它就是在授权操作它
//  2. 智能体资源绑定 —— Agent「资源」页绑定的主机组（平台原有机制）
//
// 优先级 1 必须存在：否则用户在运维台选了 web-01 开会话，工具却拿「Agent 没绑定主机组」
// 把调用拒掉，界面上就只剩一句看不懂的报错。
//
// 注意：这里刻意不要求「聊天用户是主机拥有者/管理员」。
// 智能体工具调用以智能体绑定的主机组为授权边界；
// 主机归属限制只用于人工直接操作（WebSocket 终端 / 命令执行，见 handler/host.go）。
func checkHostAccess(ctx context.Context, st *store.Store, host *model.Host) error {
	scope, err := agentscope.RequireScope(ctx)
	if err != nil {
		return err
	}
	// 1. 会话作用域优先
	switch scope.HostScopeType {
	case model.SessionScopeHost:
		if scope.HostScopeID == host.ID {
			return nil
		}
		return fmt.Errorf("当前会话绑定的是另一台主机，不能操作「%s」；如需操作它，请在左侧切换到这台主机后再开会话", host.Name)
	case model.SessionScopeHostGroup:
		if scope.HostScopeID > 0 && host.GroupID == scope.HostScopeID {
			return nil
		}
		return fmt.Errorf("主机「%s」不在当前会话的主机组内", host.Name)
	}

	// 2. 智能体资源绑定
	groupIDs, err := st.ListBoundResourceIDs(ctx, scope.AgentID, model.ResourceTypeHostGroup)
	if err != nil {
		return fmt.Errorf("加载主机组授权失败: %w", err)
	}
	authorized := false
	for _, gid := range groupIDs {
		if gid == host.GroupID {
			authorized = true
			break
		}
	}
	if !authorized {
		return fmt.Errorf("无权限操作该主机（智能体未绑定该主机所在主机组）")
	}
	return nil
}

// hostUnreachable 判断主机状态是否明确不可达。
//
// pending 表示「还没检测过」（多数是刚添加的主机），不能据此拒绝 ——
// 平台没有主动巡检任务，一刀切拒绝会让新主机永远用不了。
// 让 SSH 连接尝试说话：连不上会返回明确的连接错误，比一句「主机不在线: pending」有用得多。
func hostUnreachable(host *model.Host) bool {
	return host.Status == model.HostStatusOffline || host.Status == model.HostStatusFailed
}

// scopedHosts 列出当前会话可见的主机。
// 有会话作用域时只返回作用域内的主机，否则返回智能体绑定主机组下的主机。
func scopedHosts(ctx context.Context, st *store.Store) ([]*model.Host, error) {
	scope, err := agentscope.RequireScope(ctx)
	if err != nil {
		return nil, err
	}

	// 单机会话：直接锁定这一台，拿不到就是主机已被删除
	if scope.HostScopeType == model.SessionScopeHost {
		host, err := st.GetHost(ctx, scope.HostScopeID)
		if err != nil || host == nil {
			return nil, fmt.Errorf("当前会话绑定的主机已不存在，请重新选择主机")
		}
		return []*model.Host{host}, nil
	}

	// 主机组会话：组内在线主机
	if scope.HostScopeType == model.SessionScopeHostGroup && scope.HostScopeID > 0 {
		return st.GetHostsByGroups(ctx, []int64{scope.HostScopeID})
	}

	// 全局会话：智能体绑定的主机组
	groupIDs, err := st.ListBoundResourceIDs(ctx, scope.AgentID, model.ResourceTypeHostGroup)
	if err != nil {
		return nil, fmt.Errorf("加载主机组授权失败: %w", err)
	}
	if len(groupIDs) == 0 {
		return []*model.Host{}, nil
	}
	return st.GetHostsByGroups(ctx, groupIDs)
}

// getInt64 从 map 中取 int64 参数。
func getInt64(args map[string]any, key string, def int64) int64 {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int64:
		return val
	case int:
		return int64(val)
	}
	return def
}

// 说明：这里原本有一个 canOperateHost(scope, host)，要求「聊天用户必须是主机拥有者或管理员」。
// 已移除——智能体工具调用只以智能体绑定的主机组 / 会话作用域为授权边界；
// 主机归属限制属于人工直接操作的范畴，见 handler/host.go 的同名函数。

// getBool 从 map 中取 bool 参数。
func getBool(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}
