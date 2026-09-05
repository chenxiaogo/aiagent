package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"aiagent/internal/approval"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/internal/toolkit"
	"aiagent/pkg/sshx"
)

// connectHost 校验对主机的授权（与 checkHostAccess 同一套规则），并建立 SSH 连接。
// needOnline 为 true 时，只有明确离线的主机会被拦下；pending（未检测）放行，由连接结果说话。
// 调用方需 defer client.Close()。
func (r *AgentRuntime) connectHost(ctx context.Context, st *store.Store, hostID int64, needOnline bool) (*sshx.Client, *model.Host, error) {
	if hostID <= 0 {
		return nil, nil, fmt.Errorf("host_id 必填")
	}
	host, err := st.GetHost(ctx, hostID)
	if err != nil {
		return nil, nil, fmt.Errorf("主机不存在: %w", err)
	}
	// 与 exec_command / list_dir 等基础工具共用授权判定：会话作用域优先，其次智能体绑定
	if err := checkHostAccess(ctx, st, host); err != nil {
		return nil, nil, err
	}
	if needOnline && hostUnreachable(host) {
		return nil, nil, fmt.Errorf("主机状态为「%s」，请先确认主机可达后再试", host.Status)
	}
	client, err := sshx.Dial(sshx.HostConfig{
		Hostname:   host.Hostname,
		Port:       host.Port,
		Username:   host.Username,
		Password:   host.Password,
		PrivateKey: host.PrivateKey,
		Passphrase: host.Passphrase,
		Timeout:    15 * time.Second,
	})
	if err != nil {
		// 连接失败：把主机标记为 failed，下次调用前就能提前拦下，省一次握手超时
		_ = st.UpdateHostStatus(ctx, host.ID, model.HostStatusFailed)
		return nil, nil, fmt.Errorf("连接主机失败: %w", err)
	}
	// 连上了就证明可达：顺手把 pending / failed 纠正为 online。
	// 平台没有主机巡检任务，不这样做新添加的主机永远停在 pending。
	if host.Status != model.HostStatusOnline {
		if err := st.UpdateHostStatus(ctx, host.ID, model.HostStatusOnline); err == nil {
			host.Status = model.HostStatusOnline
		}
	}
	return client, host, nil
}

// runOnHost 在已建立的 SSH 连接上执行命令并返回结构化结果（与 exec_command 一致的输出形态）。
func runOnHost(ctx context.Context, client *sshx.Client, command string, timeoutSec int) (map[string]any, error) {
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	res, execErr := client.Exec(execCtx, command)
	if res == nil {
		return nil, execErr
	}
	return map[string]any{
		"exit_code":   res.ExitCode,
		"stdout":      truncate(res.Stdout, 8000),
		"stderr":      truncate(res.Stderr, 2000),
		"duration_ms": res.DurationMs,
		"success":     execErr == nil,
	}, nil
}

// safeToken 仅保留 shell 标识符安全字符，用于拼接 systemctl / ping / /dev/tcp 等单参数，防止命令注入。
func safeToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '-' || r == '.' || r == ':' || r == '@' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// opsHostTools 返回 1Shell 对齐的运维主机操作工具（指标/网络/进程/服务/探针/脚本/下载）。
// 与 opsBuiltinTools 中的基础工具（exec_command/list_dir/read_file/write_file）共用权限与 SSH 基础设施。
func (r *AgentRuntime) opsHostTools(ctx context.Context, st *store.Store) map[string]toolkit.Handler {
	if st == nil {
		return nil
	}
	return map[string]toolkit.Handler{
		// ---------- 主机指标类（只读） ----------
		"host_cpu_info": func(ctx context.Context, args map[string]any) (any, error) {
			client, host, err := r.connectHost(ctx, st, getInt64(args, "host_id", 0), true)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			m, err := runOnHost(ctx, client, "lscpu 2>/dev/null; echo '===CORES==='; nproc 2>/dev/null; echo '===LOAD==='; cat /proc/loadavg 2>/dev/null; echo '===CPU_USAGE==='; top -bn2 -d1 2>/dev/null | awk '/%Cpu/{line=$0} END{print line}'", 30)
			if err != nil {
				return nil, err
			}
			m["host_id"], m["host_name"] = host.ID, host.Name
			return m, nil
		},

		"host_mem_info": func(ctx context.Context, args map[string]any) (any, error) {
			client, host, err := r.connectHost(ctx, st, getInt64(args, "host_id", 0), true)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			m, err := runOnHost(ctx, client, "free -h 2>/dev/null; echo '===MEMINFO==='; head -5 /proc/meminfo 2>/dev/null", 30)
			if err != nil {
				return nil, err
			}
			m["host_id"], m["host_name"] = host.ID, host.Name
			return m, nil
		},

		"host_disk_info": func(ctx context.Context, args map[string]any) (any, error) {
			client, host, err := r.connectHost(ctx, st, getInt64(args, "host_id", 0), true)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			m, err := runOnHost(ctx, client, "df -h 2>/dev/null; echo '===LSBLK==='; lsblk 2>/dev/null", 30)
			if err != nil {
				return nil, err
			}
			m["host_id"], m["host_name"] = host.ID, host.Name
			return m, nil
		},

		"host_network_info": func(ctx context.Context, args map[string]any) (any, error) {
			client, host, err := r.connectHost(ctx, st, getInt64(args, "host_id", 0), true)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			m, err := runOnHost(ctx, client, "ip -brief addr 2>/dev/null; echo '===ROUTE==='; ip route 2>/dev/null; echo '===LISTEN==='; ss -tunlp 2>/dev/null | head -20", 30)
			if err != nil {
				return nil, err
			}
			m["host_id"], m["host_name"] = host.ID, host.Name
			return m, nil
		},

		"host_process_list": func(ctx context.Context, args map[string]any) (any, error) {
			client, host, err := r.connectHost(ctx, st, getInt64(args, "host_id", 0), true)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			m, err := runOnHost(ctx, client, "ps aux --sort=-%cpu 2>/dev/null | head -20", 30)
			if err != nil {
				return nil, err
			}
			m["host_id"], m["host_name"] = host.ID, host.Name
			return m, nil
		},

		"host_env": func(ctx context.Context, args map[string]any) (any, error) {
			client, host, err := r.connectHost(ctx, st, getInt64(args, "host_id", 0), true)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			m, err := runOnHost(ctx, client, "env 2>/dev/null | sort", 20)
			if err != nil {
				return nil, err
			}
			m["host_id"], m["host_name"] = host.ID, host.Name
			return m, nil
		},

		"host_service_status": func(ctx context.Context, args map[string]any) (any, error) {
			client, host, err := r.connectHost(ctx, st, getInt64(args, "host_id", 0), true)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			svc := safeToken(getString(args, "service", ""))
			if svc == "" {
				return nil, fmt.Errorf("service 必填（仅允许字母数字及 _-.:@）")
			}
			cmd := fmt.Sprintf("systemctl is-active %s 2>/dev/null; echo '===STATUS==='; systemctl status %s --no-pager 2>/dev/null | head -20", svc, svc)
			m, err := runOnHost(ctx, client, cmd, 20)
			if err != nil {
				return nil, err
			}
			m["host_id"], m["host_name"], m["service"] = host.ID, host.Name, svc
			return m, nil
		},

		// ---------- 网络探针（只读） ----------
		"host_probe": func(ctx context.Context, args map[string]any) (any, error) {
			client, host, err := r.connectHost(ctx, st, getInt64(args, "host_id", 0), true)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			target := safeToken(getString(args, "target", ""))
			if target == "" {
				return nil, fmt.Errorf("target 必填（仅允许字母数字及 _-.:@）")
			}
			mode := getString(args, "mode", "ping")
			var cmd string
			switch mode {
			case "tcp":
				port := getInt64(args, "port", 80)
				cmd = fmt.Sprintf("timeout 5 bash -c 'cat < /dev/null > /dev/tcp/%s/%d' && echo 'TCP OK' || echo 'TCP FAIL'", target, port)
			case "http":
				port := getInt64(args, "port", 80)
				cmd = fmt.Sprintf("curl -sS -o /dev/null -w 'HTTP %%{http_code}\\n' --max-time 5 http://%s:%d/", target, port)
			default:
				cmd = fmt.Sprintf("ping -c 3 -W 2 %s", target)
			}
			m, err := runOnHost(ctx, client, cmd, 20)
			if err != nil {
				return nil, err
			}
			m["host_id"], m["host_name"], m["target"], m["mode"] = host.ID, host.Name, target, mode
			return m, nil
		},

		// ---------- 文件下载（只读，返回 base64） ----------
		"host_download_file": func(ctx context.Context, args map[string]any) (any, error) {
			client, host, err := r.connectHost(ctx, st, getInt64(args, "host_id", 0), true)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			path := getString(args, "path", "")
			if path == "" {
				return nil, fmt.Errorf("path 必填")
			}
			content, err := client.ReadFile(ctx, path, 0)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"host_id":         host.ID,
				"path":            path,
				"content_base64":  base64.StdEncoding.EncodeToString([]byte(content)),
				"size":            len(content),
			}, nil
		},

		// ---------- 脚本执行（有副作用，需审批） ----------
		"host_run_script": func(ctx context.Context, args map[string]any) (any, error) {
			hostID := getInt64(args, "host_id", 0)
			script := getString(args, "script", "")
			if strings.TrimSpace(script) == "" {
				return nil, fmt.Errorf("script 必填")
			}
			// 红线兜底：脚本内容同样走 exec_command 的灾难命令拦截
			if assessment := approval.AssessToolCall("exec_command", map[string]any{"command": script}); assessment.Block {
				return nil, fmt.Errorf("%s", assessment.Reason)
			}
			client, host, err := r.connectHost(ctx, st, hostID, true)
			if err != nil {
				return nil, err
			}
			defer client.Close()
			interpreter := safeToken(getString(args, "interpreter", "bash"))
			if interpreter == "" {
				interpreter = "bash"
			}
			tmp := fmt.Sprintf("/tmp/aiagent_script_%d.sh", time.Now().UnixNano())
			if err := client.WriteFile(ctx, tmp, script, false); err != nil {
				return nil, fmt.Errorf("写入脚本失败: %w", err)
			}
			defer client.Exec(context.Background(), "rm -f "+tmp)
			m, err := runOnHost(ctx, client, fmt.Sprintf("%s %s", interpreter, tmp), 120)
			if err != nil {
				return nil, err
			}
			m["host_id"], m["host_name"], m["script_path"] = host.ID, host.Name, tmp
			return m, nil
		},
	}
}
