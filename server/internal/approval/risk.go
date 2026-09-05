package approval

import (
	"regexp"
	"strings"
)

// 风险等级。
const (
	RiskNone     = ""         // 无需确认（只读类操作）
	RiskMedium   = "medium"   // 需要在聊天框由用户确认后执行
	RiskHigh     = "high"     // 高风险操作，同样需确认，前端高亮提示
	RiskCritical = "critical" // 红线：不可逆的灾难性操作，直接拒绝，不再询问
)

// Assessment 工具调用的风险评估结论。
type Assessment struct {
	Risk   string // 见 Risk* 常量
	Reason string // 给用户的中文原因
	Block  bool   // true 表示红线命令，直接拒绝执行（不进入确认流程）
}

// ---------- 红线：不可逆的灾难性操作（参考 1Shell command-safety.js）----------

var catastrophicRules = []struct {
	id    string
	label string
	regex *regexp.Regexp
}{
	{id: "fork-bomb", label: "fork bomb（耗尽系统资源）",
		regex: regexp.MustCompile(`:\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;?\s*:`)},
	{id: "mkfs", label: "格式化块设备 (mkfs)",
		regex: regexp.MustCompile(`\bmkfs(\.\w+)?\b[^\n|;&]*/dev/`)},
	{id: "dd-to-device", label: "向块设备底层写入 (dd of=/dev/...)",
		regex: regexp.MustCompile(`\bdd\b[^\n]*\bof=/dev/(sd|nvme|vd|hd|disk|mmcblk|loop)`)},
	{id: "wipe-block-device", label: "直接覆写块设备 (>/dev/sd*)",
		regex: regexp.MustCompile(`>\s*/dev/(sd|nvme|vd|hd|disk|mmcblk|loop)`)},
	{id: "no-preserve-root", label: "显式删除根目录 (--no-preserve-root)",
		regex: regexp.MustCompile(`--no-preserve-root\b`)},
	{id: "rm-root", label: "递归删除根目录或全盘通配 (rm -rf /)",
		regex: regexp.MustCompile(`\brm\b[^\n|;&]*(\s-{1,2}[a-z]*r[a-z]*|--recursive)[^\n|;&]*\s/(\s|$|\*|[|;&])`)},
	{id: "rm-global-wildcard", label: "递归删除当前目录全部内容",
		regex: regexp.MustCompile(`\brm\b[^\n|;&]*(\s-{1,2}[a-z]*r[a-z]*|--recursive)[^\n|;&]*\s\*\s*($|[|;&])`)},
	// Windows 系命令与盘符大小写不敏感（Format C: / FORMAT c: / HKLM 都算），统一加 (?i)
	{id: "win-format", label: "格式化磁盘卷 (format)",
		regex: regexp.MustCompile(`(?i)(^|[|;&\n]|\s)format(\.com)?\s+["']?[a-z]:`)},
	{id: "win-format-volume", label: "格式化磁盘卷 (Format-Volume)",
		regex: regexp.MustCompile(`(?i)\bformat-volume\b`)},
	{id: "win-disk-wipe", label: "磁盘分区抹除 (diskpart clean / Clear-Disk)",
		regex: regexp.MustCompile(`(?i)(\bclear-disk\b|\bremove-partition\b|(\bdiskpart\b[^\n]*(/s\b|-s\b|\bclean\b)))`)},
	{id: "win-system-delete", label: "递归删除 Windows 盘根或系统目录",
		regex: regexp.MustCompile(`(?i)\b(remove-item|ri|rd|rmdir|del|erase)\b[^\n]*(-r(ecurse)?\b|/s\b)[^\n]*[a-z]:([\\/]+(windows|program files|programdata|users)?)?([\\/]+\*?)?(\s|$)`)},
	{id: "win-registry-hive", label: "删除注册表根配置单元",
		regex: regexp.MustCompile(`(?i)(\breg(\.exe)?\s+delete\b[^\n]*\bhk(lm|cu|cr|u|cc)\b)|(\bremove-item\b[^\n]*-r(ecurse)?\b[^\n]*\bhk(lm|cu|cr|u|cc)\b)`)},
	{id: "win-boot-destroy", label: "破坏系统引导配置 (bcdedit)",
		regex: regexp.MustCompile(`(?i)\bbcdedit\b[^\n]*/(delete|deletevalue)\b[^\n]*\{?(default|current|bootmgr)`)},
	{id: "win-shadow-copy", label: "删除卷影副本（不可回滚）",
		regex: regexp.MustCompile(`(?i)(\bvssadmin\b[^\n]*\bdelete\b[^\n]*\bshadows\b)|(\bwmic\b[^\n]*\bshadowcopy\b[^\n]*\bdelete\b)`)},
}

// ---------- 高风险：合法但影响面大的运维操作（需确认，前端高亮）----------

var highRiskRules = []struct {
	label string
	regex *regexp.Regexp
}{
	{label: "关机 / 重启", regex: regexp.MustCompile(`\b(shutdown|reboot|halt|poweroff|init\s+[06])\b`)},
	{label: "强制结束进程", regex: regexp.MustCompile(`\b(kill\s+-9|killall|pkill)\b`)},
	{label: "停止 / 禁用系统服务", regex: regexp.MustCompile(`\bsystemctl\s+(stop|disable|mask)\b|\bservice\s+\S+\s+stop\b`)},
	{label: "递归删除文件", regex: regexp.MustCompile(`\brm\b[^\n|;&]*(\s-{1,2}[a-z]*[rf]|--recursive|--force)\b`)},
	{label: "修改文件权限 / 属主", regex: regexp.MustCompile(`\b(chmod\s+(-R\s+)?777|chown\s+-R|chattr)\b`)},
	{label: "变更系统账号", regex: regexp.MustCompile(`\b(userdel|usermod|passwd|groupdel)\b`)},
	{label: "清空防火墙规则", regex: regexp.MustCompile(`\b(iptables\s+-F|iptables\s+--flush|ufw\s+reset)\b`)},
	{label: "删除计划任务", regex: regexp.MustCompile(`\bcrontab\b[^\n]*\s-r\b`)},
	{label: "丢弃数据库数据", regex: regexp.MustCompile(`\b(drop\s+database|drop\s+table|truncate\s+table)\b|\bredis-cli\b[^\n]*\bflush(all|db)\b`)},
	{label: "远程脚本直执行", regex: regexp.MustCompile(`\b(curl|wget)\b[^\n]*\|\s*(sudo\s+)?(ba)?sh\b`)},
}

// 写入文件的敏感路径：覆盖这些位置会让系统或关键服务直接失效。
var sensitiveWritePrefixes = []string{
	"/etc/", "/boot/", "/sys/", "/proc/", "/dev/", "/bin/", "/sbin/", "/usr/bin/", "/usr/sbin/",
	"/lib/", "/lib64/", "/usr/lib/", "/root/.ssh/", "/etc", "/boot", "/sys", "/proc", "/dev", "/bin", "/sbin", "/usr",
}

// AssessToolCall 评估一次工具调用的风险。
// 只读工具返回空风险；写操作默认 medium（需确认）；
// 命中红线返回 Block=true，调用方应直接拒绝而不进入确认流程。
func AssessToolCall(toolName string, args map[string]any) Assessment {
	switch toolName {
	case "exec_command":
		return assessCommand(stringArg(args, "command"))
	case "write_file":
		return assessWritePath(stringArg(args, "path"))
	default:
		return Assessment{}
	}
}

// assessCommand 评估 shell 命令风险。
func assessCommand(command string) Assessment {
	raw := strings.TrimSpace(command)
	if raw == "" {
		return Assessment{}
	}
	normalized := normalizeForRiskMatch(raw)

	// 红线：同时匹配原文与归一化文本（归一化可识别 rm -rf "/" 这类带引号写法）
	for _, rule := range catastrophicRules {
		if rule.regex.MatchString(raw) || rule.regex.MatchString(normalized) {
			return Assessment{Risk: RiskCritical, Reason: "已拦截不可逆的灾难性命令：" + rule.label, Block: true}
		}
	}

	// 任何命令执行都改变服务器状态，至少要经过确认
	assessment := Assessment{Risk: RiskMedium, Reason: "该命令会在主机上执行"}
	for _, rule := range highRiskRules {
		if rule.regex.MatchString(normalized) {
			assessment.Risk = RiskHigh
			assessment.Reason = "高风险操作：" + rule.label
			break
		}
	}
	return assessment
}

// assessWritePath 评估文件写入路径风险。
func assessWritePath(path string) Assessment {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return Assessment{Risk: RiskMedium, Reason: "该操作会写入远程文件"}
	}
	normalized := strings.ToLower(strings.ReplaceAll(trimmed, "\\", "/"))
	for _, prefix := range sensitiveWritePrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return Assessment{Risk: RiskHigh, Reason: "写入系统敏感路径：" + trimmed}
		}
	}
	return Assessment{Risk: RiskMedium, Reason: "该操作会写入远程文件：" + trimmed}
}

// normalizeForRiskMatch 归一化命令文本：剥掉包裹路径的引号与反斜杠转义，
// 使 rm -rf "/"、rm -rf \/ 这类写法也能被红线规则识别。仅用于匹配，不改变实际执行的命令。
func normalizeForRiskMatch(text string) string {
	out := quotedStringRegex.ReplaceAllString(text, "$1")
	out = singleQuotedRegex.ReplaceAllString(out, "$1")
	out = strings.ReplaceAll(out, `\`, "")
	return out
}

var (
	quotedStringRegex = regexp.MustCompile(`"([^"]*)"`)
	singleQuotedRegex = regexp.MustCompile(`'([^']*)'`)
)

// stringArg 从工具参数中取字符串。
func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
