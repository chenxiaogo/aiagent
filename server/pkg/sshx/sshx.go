package sshx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// HostConfig 主机连接配置。
type HostConfig struct {
	Hostname   string
	Port       int
	Username   string
	Password   string // 密码认证时使用
	PrivateKey string // 私钥内容（PEM 格式）
	Passphrase string // 私钥口令（可选）
	Timeout    time.Duration
}

// ExecResult 命令执行结果。
type ExecResult struct {
	ExitCode   int
	Stdout     string
	Stderr     string
	DurationMs int64
}

// Client SSH 客户端封装。
type Client struct {
	client *ssh.Client
}

// Dial 建立 SSH 连接。
func Dial(cfg HostConfig) (*Client, error) {
	if cfg.Port <= 0 {
		cfg.Port = 22
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	var authMethod ssh.AuthMethod
	if cfg.PrivateKey != "" {
		var signer ssh.Signer
		var err error
		if cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(cfg.PrivateKey), []byte(cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		authMethod = ssh.PublicKeys(signer)
	} else {
		authMethod = ssh.Password(cfg.Password)
	}
	sshCfg := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            []ssh.AuthMethod{authMethod},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: 生产环境应校验 host key
		Timeout:         cfg.Timeout,
	}
	// 用 JoinHostPort 而不是 fmt.Sprintf("%s:%d")：后者拼出的 IPv6 地址缺少方括号，会导致连接失败
	addr := net.JoinHostPort(cfg.Hostname, strconv.Itoa(cfg.Port))
	conn, err := net.DialTimeout("tcp", addr, cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	return &Client{client: ssh.NewClient(sshConn, chans, reqs)}, nil
}

// Close 关闭连接。
func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// ShellSession SSH 交互式 Shell（PTY）会话。
// 用于 WebSocket 终端：调用方通过 Write 写入 stdin，通过 Output 读取 stdout/stderr 混合流。
type ShellSession struct {
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	closed  bool
	mu      sync.Mutex
}

// ShellOpts PTY 参数。
type ShellOpts struct {
	Term   string // 终端类型，默认 xterm-256color
	Rows   int    // 行数，默认 40
	Cols   int    // 列数，默认 120
	Width  int    // 像素宽，可选
	Height int    // 像素高，可选
}

// OpenShell 打开一个交互式 Shell 会话（PTY）。
func (c *Client) OpenShell(opts ShellOpts) (*ShellSession, error) {
	if opts.Term == "" {
		opts.Term = "xterm-256color"
	}
	if opts.Rows <= 0 {
		opts.Rows = 40
	}
	if opts.Cols <= 0 {
		opts.Cols = 120
	}

	sess, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}

	// 请求 PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty(opts.Term, opts.Rows, opts.Cols, modes); err != nil {
		sess.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	// stderr 重定向到 stdout（PTY 模式下通常是混合的）
	sess.Stderr = nil

	if err := sess.Shell(); err != nil {
		sess.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	return &ShellSession{
		session: sess,
		stdin:   stdin,
		stdout:  stdout,
	}, nil
}

// Write 写入 stdin。
func (s *ShellSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.EOF
	}
	return s.stdin.Write(p)
}

// Read 读取 stdout。
func (s *ShellSession) Read(p []byte) (int, error) {
	return s.stdout.Read(p)
}

// Resize 调整终端窗口大小（行, 列）。
func (s *ShellSession) Resize(rows, cols int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("session closed")
	}
	if rows <= 0 || cols <= 0 {
		return nil
	}
	return s.session.WindowChange(rows, cols)
}

// Wait 等待会话结束并返回错误。
func (s *ShellSession) Wait() error {
	return s.session.Wait()
}

// Close 关闭会话。
func (s *ShellSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.stdin.Close()
	return s.session.Close()
}

// Exec 执行命令，返回完整输出。
func (c *Client) Exec(ctx context.Context, command string) (*ExecResult, error) {
	start := time.Now()
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	var runErr error
	select {
	case <-ctx.Done():
		session.Signal(ssh.SIGKILL)
		runErr = ctx.Err()
	case runErr = <-done:
	}

	exitCode := -1
	if runErr == nil {
		exitCode = 0
	} else if exitErr, ok := runErr.(*ssh.ExitError); ok {
		exitCode = exitErr.ExitStatus()
	}

	return &ExecResult{
		ExitCode:   exitCode,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMs: time.Since(start).Milliseconds(),
	}, runErr
}

// ExecStream 执行命令并实时回传输出（用于 WebSocket 流式命令执行，参考 1Shell host_exec）。
// onChunk 在 stdout/stderr 产生数据时被调用（stream 为 "stdout" / "stderr"），调用方负责转发。
// 同时仍返回完整 ExecResult 便于审计。命令受 ctx 超时/取消控制（超时发 SIGKILL）。
func (c *Client) ExecStream(ctx context.Context, command string, onChunk func(stream string, data []byte)) (*ExecResult, error) {
	start := time.Now()
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	var stdout, stderr bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, rerr := stdoutPipe.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				stdout.Write(chunk)
				if onChunk != nil {
					onChunk("stdout", chunk)
				}
			}
			if rerr != nil {
				break
			}
		}
	}()
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, rerr := stderrPipe.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				stderr.Write(chunk)
				if onChunk != nil {
					onChunk("stderr", chunk)
				}
			}
			if rerr != nil {
				break
			}
		}
	}()

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	var runErr error
	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		runErr = ctx.Err()
	case runErr = <-done:
	}

	wg.Wait()

	exitCode := -1
	if runErr == nil {
		exitCode = 0
	} else if exitErr, ok := runErr.(*ssh.ExitError); ok {
		exitCode = exitErr.ExitStatus()
	}

	return &ExecResult{
		ExitCode:   exitCode,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMs: time.Since(start).Milliseconds(),
	}, runErr
}

// ListDir 列出远程目录内容。
//
// 用 stat 输出「|」分隔的字段，而不是解析 ls -la：按空格切分会把含空格的文件名截断，
// 这是文件管理器里最常见的翻车点（"my file.txt" 只剩 "my"）。
func (c *Client) ListDir(ctx context.Context, path string) ([]DirEntry, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	cmd := fmt.Sprintf(
		"cd %s 2>/dev/null && ls -A | while IFS= read -r f; do stat -c '%%n|%%F|%%s|%%Y|%%A|%%U|%%G' -- \"$f\" 2>/dev/null; done",
		escapePath(path))
	output, err := session.Output(cmd)
	if err != nil {
		return nil, fmt.Errorf("list dir: %w", err)
	}
	return parseStatOutput(string(output)), nil
}

// HomeDir 返回该连接账号的家目录。
//
// 文件管理必须以账号家目录为起点：直接用 "/" 时，非 root 账号（ubuntu / deploy 等）
// 会因为无权限而整页报错，用户会误以为「文件功能坏了」。
// exec session 的默认工作目录就是家目录，pwd 足够可靠；拿不到再退回 $HOME。
func (c *Client) HomeDir(ctx context.Context) (string, error) {
	res, err := c.Exec(ctx, "pwd")
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(res.Stdout)
	if dir == "" {
		res2, err2 := c.Exec(ctx, "echo $HOME")
		if err2 != nil {
			return "", err2
		}
		dir = strings.TrimSpace(res2.Stdout)
	}
	if dir == "" || !strings.HasPrefix(dir, "/") {
		return "", fmt.Errorf("无法获取账号家目录")
	}
	return path.Clean(dir), nil
}

// DownloadStream 打开远程文件的读取流，二进制安全，适合大文件直接往 HTTP 响应里灌。
// 返回的 cleanup 必须被调用，用于释放 SSH 会话。
func (c *Client) DownloadStream(ctx context.Context, path string) (io.ReadCloser, func(), error) {
	session, err := c.client.NewSession()
	if err != nil {
		return nil, nil, fmt.Errorf("new session: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := session.Start(fmt.Sprintf("cat %s", escapePath(path))); err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("start download: %w", err)
	}
	cleanup := func() { session.Close() }
	// StdoutPipe 只返回 io.Reader，包一层让调用方能直接 Close（关闭会话即结束读取）
	return struct {
		io.Reader
		io.Closer
	}{Reader: stdout, Closer: session}, cleanup, nil
}

// UploadStream 打开远程文件的写入流（cat > path），二进制安全。
// 写入完成后必须调用 finish：它会关闭 stdin 并等待远端命令结束，返回写入是否成功。
// 与 WriteFile 不同，这里不自带备份，备份由调用方决定（会先写临时文件再改名）。
func (c *Client) UploadStream(ctx context.Context, path string) (io.WriteCloser, func() error, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return nil, nil, fmt.Errorf("new session: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("stdin pipe: %w", err)
	}
	if err := session.Start(fmt.Sprintf("cat > %s", escapePath(path))); err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("start upload: %w", err)
	}
	finish := func() error {
		_ = stdin.Close()
		waitErr := session.Wait()
		session.Close()
		if waitErr != nil {
			return fmt.Errorf("写入远端失败: %w", waitErr)
		}
		return nil
	}
	return stdin, finish, nil
}

// ReadFile 读取远程文件内容（文本，上限 8MB）。
func (c *Client) ReadFile(ctx context.Context, path string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 8 * 1024 * 1024 // 8MB
	}
	session, err := c.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	cmd := fmt.Sprintf("head -c %d %s", maxBytes, escapePath(path))
	output, err := session.Output(cmd)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(output), nil
}

// WriteFile 写入远程文件（文本，支持 backup）。
func (c *Client) WriteFile(ctx context.Context, path, content string, backup bool) error {
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	// 备份
	if backup {
		backupCmd := fmt.Sprintf("cp %s %s.bak.$(date +%%s) 2>/dev/null || true", escapePath(path), escapePath(path))
		session.Run(backupCmd)
	}

	// 通过 stdin 写入，避免内容里特殊字符导致问题
	wc, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	if err := session.Start(fmt.Sprintf("cat > %s", escapePath(path))); err != nil {
		return fmt.Errorf("start write: %w", err)
	}
	if _, err := io.WriteString(wc, content); err != nil {
		return err
	}
	wc.Close()
	return session.Wait()
}

// DirEntry 目录条目。
type DirEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // file / dir / link
	Size        int64  `json:"size"`
	Permission  string `json:"permission"`
	Owner       string `json:"owner"`
	Group       string `json:"group"`
	Modified    string `json:"modified"`
}

// parseStatOutput 解析 stat 输出：name|type|size|mtime|perm|owner|group。
func parseStatOutput(output string) []DirEntry {
	var entries []DirEntry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "|", 7)
		if len(fields) < 7 {
			continue
		}
		name := fields[0]
		if name == "." || name == ".." {
			continue
		}
		typ := "file"
		switch {
		case strings.Contains(fields[1], "directory"):
			typ = "dir"
		case strings.Contains(fields[1], "symbolic"):
			typ = "link"
		}
		modified := ""
		if sec, err := strconv.ParseInt(fields[3], 10, 64); err == nil && sec > 0 {
			modified = time.Unix(sec, 0).Format("2006-01-02 15:04")
		}
		entries = append(entries, DirEntry{
			Name:       name,
			Type:       typ,
			Size:       parseInt64(fields[2]),
			Permission: fields[4],
			Owner:      fields[5],
			Group:      fields[6],
			Modified:   modified,
		})
	}
	// 目录在前、同类型按名称升序，和常见文件管理器一致
	sort.SliceStable(entries, func(i, j int) bool {
		if (entries[i].Type == "dir") != (entries[j].Type == "dir") {
			return entries[i].Type == "dir"
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

func parseInt64(s string) int64 {
	var n int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		}
	}
	return n
}

// escapePath 简易路径转义（防止空格和特殊字符）。
func escapePath(path string) string {
	return "'" + string(bytes.ReplaceAll([]byte(path), []byte("'"), []byte("'\\''"))) + "'"
}

// QuotePath 导出路径转义，供上层拼接 shell 命令时使用（避免各处自己拼引号出错）。
func QuotePath(path string) string {
	return escapePath(path)
}

// ExecChecked 执行一条命令，仅在退出码非 0 时返回错误（把 stderr 带进错误信息）。
func (c *Client) ExecChecked(ctx context.Context, command string) error {
	res, err := c.Exec(ctx, command)
	if res != nil {
		if res.ExitCode != 0 {
			msg := strings.TrimSpace(res.Stderr)
			if msg == "" {
				msg = strings.TrimSpace(res.Stdout)
			}
			if msg == "" {
				return fmt.Errorf("命令退出码 %d", res.ExitCode)
			}
			return fmt.Errorf("命令退出码 %d: %s", res.ExitCode, msg)
		}
		return nil
	}
	return err
}
