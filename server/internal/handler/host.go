package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aiagent/internal/approval"
	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
	"aiagent/pkg/sshx"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// HostHandler 运维主机管理。
type HostHandler struct {
	store *store.Store
}

func NewHostHandler(s *store.Store) *HostHandler {
	return &HostHandler{store: s}
}

func (h *HostHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/hosts")
	{
		// 主机组（写操作需主机管理权限）
		group.GET("/groups", h.ListGroups)
		group.POST("/groups", middleware.RequirePerm(model.PermNodeManage), h.CreateGroup)
		group.PUT("/groups/:id", middleware.RequirePerm(model.PermNodeManage), h.UpdateGroup)
		group.DELETE("/groups/:id", middleware.RequirePerm(model.PermNodeManage), h.DeleteGroup)

		// 主机（写操作需主机管理权限）
		group.GET("", h.ListHosts)
		group.GET("/:id", h.GetHost)
		group.POST("", middleware.RequirePerm(model.PermNodeManage), h.CreateHost)
		group.PUT("/:id", middleware.RequirePerm(model.PermNodeManage), h.UpdateHost)
		group.DELETE("/:id", middleware.RequirePerm(model.PermNodeManage), h.DeleteHost)

		// 命令记录
		group.GET("/:id/commands", h.ListCommandRecords)

		// 操作审计（参考 1Shell auditService）
		group.GET("/audit", h.ListAudits)

		// WebSocket 终端（需命令执行权限）
		group.GET("/:id/terminal", middleware.RequirePerm(model.PermHostExec), h.Terminal)

		// WebSocket 流式命令执行（参考 1Shell host_exec，实时回传输出，无 60s HTTP 上限）
		group.GET("/:id/exec", middleware.RequirePerm(model.PermHostExec), h.ExecWS)

		// 文件管理（列目录 / 上传 / 下载 / 新建目录 / 删除 / 重命名）
		group.GET("/:id/files", middleware.RequirePerm(model.PermHostFile), h.ListFiles)
		group.GET("/:id/files/download", middleware.RequirePerm(model.PermHostFile), h.DownloadFile)
		group.POST("/:id/files/upload", middleware.RequirePerm(model.PermHostFile), h.UploadFile)
		group.POST("/:id/files/mkdir", middleware.RequirePerm(model.PermHostFile), h.MkdirRemote)
		group.DELETE("/:id/files", middleware.RequirePerm(model.PermHostFile), h.DeleteRemoteFile)
		group.POST("/:id/files/rename", middleware.RequirePerm(model.PermHostFile), h.RenameRemoteFile)
	}
}

var hostWsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 生产环境应校验来源
	},
}

// ---------- 主机组 ----------

// recordAudit 写入主机/主机组变更审计（参考 1Shell auditService.log）。
func (h *HostHandler) recordAudit(c *gin.Context, action, targetType string, targetID int64, targetName, detail string) {
	ctx := tracex.FromRequest(c)
	_, username, _ := middleware.CurrentUser(c)
	uid, _, _ := middleware.CurrentUser(c)
	_ = h.store.CreateHostAudit(ctx, &model.HostAuditLog{
		Action:       action,
		TargetType:   targetType,
		TargetID:     targetID,
		TargetName:   targetName,
		OperatorID:   uid,
		OperatorName: username,
		Detail:       detail,
		ClientIP:     c.ClientIP(),
	})
}

func (h *HostHandler) ListGroups(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	uid, _, _ := middleware.CurrentUser(c)
	_, _, isAdmin := middleware.CurrentUser(c)
	// 管理员查看全部主机组，普通用户仅看自己的
	ownerID := uid
	if isAdmin && c.Query("all") == "1" {
		ownerID = 0
	}
	list, err := h.store.ListHostGroups(ctx, ownerID, c.Query("keyword"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

func (h *HostHandler) CreateGroup(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	log := ilog.Trace(ctx)
	uid, username, _ := middleware.CurrentUser(c)
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "名称不能为空"})
		return
	}
	group := &model.HostGroup{
		Name:        req.Name,
		Description: req.Description,
		OwnerID:     uid,
		OwnerName:   username,
	}
	if err := h.store.CreateHostGroup(ctx, group); err != nil {
		log.Errorf("create host group failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	log.Infof("host group created: %d(%s)", group.ID, group.Name)
	h.recordAudit(c, model.HostAuditGroupCreate, "group", group.ID, group.Name, "创建主机组")
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": group})
}

func (h *HostHandler) UpdateGroup(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	log := ilog.Trace(ctx)
	uid, _, _ := middleware.CurrentUser(c)
	_, _, isAdmin := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	group, err := h.store.GetHostGroup(ctx, id)
	if err != nil || (group.OwnerID != uid && !isAdmin) {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "主机组不存在"})
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	group.Name = req.Name
	group.Description = req.Description
	if err := h.store.UpdateHostGroup(ctx, group); err != nil {
		log.Errorf("update host group failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	h.recordAudit(c, model.HostAuditGroupUpdate, "group", group.ID, group.Name, "更新主机组")
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": group})
}

func (h *HostHandler) DeleteGroup(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	log := ilog.Trace(ctx)
	uid, _, _ := middleware.CurrentUser(c)
	_, _, isAdmin := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	group, err := h.store.GetHostGroup(ctx, id)
	if err != nil || (group.OwnerID != uid && !isAdmin) {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "主机组不存在"})
		return
	}
	if err := h.store.DeleteHostGroup(ctx, id); err != nil {
		log.Errorf("delete host group failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	h.recordAudit(c, model.HostAuditGroupDelete, "group", id, group.Name, "删除主机组")
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})
}

// ---------- 主机 ----------

func (h *HostHandler) ListHosts(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	uid, _, _ := middleware.CurrentUser(c)
	_, _, isAdmin := middleware.CurrentUser(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	groupID, _ := strconv.ParseInt(c.Query("groupId"), 10, 64)

	list, total, err := h.store.ListHosts(ctx, store.HostQuery{
		OwnerID:  uid,
		GroupID:  groupID,
		Keyword:  c.Query("keyword"),
		Status:   c.Query("status"),
		Role:     c.Query("role"),
		All:      isAdmin, // 管理员查看全部主机（运维视角）
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"list":  list,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}

func (h *HostHandler) GetHost(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	uid, _, _ := middleware.CurrentUser(c)
	_, _, isAdmin := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	host, err := h.store.GetHost(ctx, id)
	if err != nil || (host.OwnerID != uid && !isAdmin) {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "主机不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": host})
}

func (h *HostHandler) CreateHost(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	log := ilog.Trace(ctx)
	uid, username, _ := middleware.CurrentUser(c)
	var req struct {
		Name        string `json:"name"`
		Hostname    string `json:"hostname"`
		Port        int    `json:"port"`
		Username    string `json:"username"`
		AuthType    string `json:"authType"`
		Password    string `json:"password"`
		PrivateKey  string `json:"privateKey"`
		Passphrase  string `json:"passphrase"`
		OS          string `json:"os"`
		Role        string `json:"role"`
		GroupID     int64  `json:"groupId"`
		Tags        string `json:"tags"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" || req.Hostname == "" || req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数不完整"})
		return
	}
	if !model.IsValidHostRole(req.Role) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "主机角色不合法"})
		return
	}
	if req.Port <= 0 {
		req.Port = 22
	}
	if req.AuthType == "" {
		req.AuthType = model.HostAuthPassword
	}
	if req.OS == "" {
		req.OS = "linux"
	}
	if req.Role == "" {
		req.Role = model.HostRoleOther
	}
	host := &model.Host{
		Name:        req.Name,
		Hostname:    req.Hostname,
		Port:        req.Port,
		Username:    req.Username,
		AuthType:    req.AuthType,
		Password:    req.Password,
		PrivateKey:  req.PrivateKey,
		Passphrase:  req.Passphrase,
		OS:          req.OS,
		Role:        req.Role,
		Status:      model.HostStatusPending,
		GroupID:     req.GroupID,
		Tags:        req.Tags,
		Description: req.Description,
		OwnerID:     uid,
		OwnerName:   username,
	}
	now := time.Now()
	host.CreatedAt, host.UpdatedAt = now, now
	// TODO: 加密存储密码/私钥（当前明文存储，后续引入加密模块）
	if err := h.store.CreateHost(ctx, host); err != nil {
		log.Errorf("create host failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	log.Infof("host created: %d(%s)", host.ID, host.Name)
	h.recordAudit(c, model.HostAuditHostCreate, "host", host.ID, host.Name, "创建主机")
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": host})
}

func (h *HostHandler) UpdateHost(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	log := ilog.Trace(ctx)
	uid, _, _ := middleware.CurrentUser(c)
	_, _, isAdmin := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	host, err := h.store.GetHost(ctx, id)
	if err != nil || (host.OwnerID != uid && !isAdmin) {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "主机不存在"})
		return
	}
	var req struct {
		Name        string `json:"name"`
		Hostname    string `json:"hostname"`
		Port        int    `json:"port"`
		Username    string `json:"username"`
		AuthType    string `json:"authType"`
		Password    string `json:"password"`
		PrivateKey  string `json:"privateKey"`
		Passphrase  string `json:"passphrase"`
		OS          string `json:"os"`
		Role        string `json:"role"`
		Status      string `json:"status"`
		GroupID     int64  `json:"groupId"`
		Tags        string `json:"tags"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if !model.IsValidHostRole(req.Role) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "主机角色不合法"})
		return
	}
	host.Name = req.Name
	host.Hostname = req.Hostname
	if req.Port > 0 {
		host.Port = req.Port
	}
	host.Username = req.Username
	host.AuthType = req.AuthType
	if req.Password != "" {
		host.Password = req.Password
	}
	if req.PrivateKey != "" {
		host.PrivateKey = req.PrivateKey
	}
	if req.Passphrase != "" {
		host.Passphrase = req.Passphrase
	}
	host.OS = req.OS
	if req.Role != "" {
		host.Role = req.Role
	}
	if req.Status != "" {
		host.Status = req.Status
	}
	host.GroupID = req.GroupID
	host.Tags = req.Tags
	host.Description = req.Description
	if err := h.store.UpdateHost(ctx, host); err != nil {
		log.Errorf("update host failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	h.recordAudit(c, model.HostAuditHostUpdate, "host", host.ID, host.Name, "更新主机")
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": host})
}

func (h *HostHandler) DeleteHost(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	log := ilog.Trace(ctx)
	uid, _, _ := middleware.CurrentUser(c)
	_, _, isAdmin := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	host, err := h.store.GetHost(ctx, id)
	if err != nil || (host.OwnerID != uid && !isAdmin) {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "主机不存在"})
		return
	}
	if err := h.store.DeleteHost(ctx, id); err != nil {
		log.Errorf("delete host failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	h.recordAudit(c, model.HostAuditHostDelete, "host", id, host.Name, "删除主机")
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})
}

// ---------- 命令记录 ----------

// ListAudits 查询主机/主机组操作审计记录（参考 1Shell auditService）。
// 支持 ?targetType=host|group & ?targetId= & ?action= 过滤。
func (h *HostHandler) ListAudits(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	targetID, _ := strconv.ParseInt(c.Query("targetId"), 10, 64)
	list, err := h.store.ListHostAudits(ctx, c.Query("targetType"), targetID, c.Query("action"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

func (h *HostHandler) ListCommandRecords(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	uid, _, _ := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	host, err := h.store.GetHost(ctx, id)
	if err != nil || host.OwnerID != uid {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "主机不存在"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	list, err := h.store.ListHostCommandRecords(ctx, id, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

// ---------- WebSocket 终端 ----------

// wsTermMsg 前端发来的消息。
type wsTermMsg struct {
	Type string          `json:"type"` // input / resize
	Data string          `json:"data"` // input 时是按键内容
	Size *wsTermSizeData `json:"size,omitempty"`
}

type wsTermSizeData struct {
	Rows   int `json:"rows"`
	Cols   int `json:"cols"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Terminal WebSocket 交互式终端。
// 消息协议（JSON）：
//   客户端 -> 服务端：
//     { type: "input", data: "按键字节" }
//     { type: "resize", size: { rows, cols, width, height } }
//   服务端 -> 客户端：
//     { type: "output", data: "终端输出" }
//     { type: "error", data: "错误信息" }
//     { type: "close" }
func (h *HostHandler) Terminal(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	log := ilog.Trace(ctx)
	uid, _, isAdmin := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	host, err := h.store.GetHost(ctx, id)
	if err != nil || !canOperateHost(uid, isAdmin, host) {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "主机不存在或无权限"})
		return
	}

	ws, err := hostWsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Errorf("ws upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	// 连接 SSH
	sshClient, err := sshx.Dial(sshx.HostConfig{
		Hostname:   host.Hostname,
		Port:       host.Port,
		Username:   host.Username,
		Password:   host.Password,
		PrivateKey: host.PrivateKey,
		Passphrase: host.Passphrase,
		Timeout:    10 * time.Second,
	})
	if err != nil {
		ws.WriteJSON(gin.H{"type": "error", "data": "连接主机失败：" + err.Error()})
		return
	}
	defer sshClient.Close()

	// 打开 Shell
	shell, err := sshClient.OpenShell(sshx.ShellOpts{Rows: 40, Cols: 120})
	if err != nil {
		ws.WriteJSON(gin.H{"type": "error", "data": "打开 Shell 失败：" + err.Error()})
		return
	}
	defer shell.Close()

	// 记录运维连接审计（参考 1Shell auditService）
	h.recordAudit(c, model.HostAuditHostTerminal, "host", id, host.Name, "打开 SSH 终端")

	done := make(chan struct{})

	// 读协程：SSH stdout -> WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := shell.Read(buf)
			if n > 0 {
				if wErr := ws.WriteMessage(websocket.TextMessage,
					[]byte(`{"type":"output","data":`+toJSONString(string(buf[:n]))+`}`)); wErr != nil {
					break
				}
			}
			if err != nil {
				if err != io.EOF {
					ws.WriteJSON(gin.H{"type": "error", "data": err.Error()})
				}
				break
			}
		}
		close(done)
	}()

	// 写协程：WebSocket -> SSH stdin
	go func() {
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				break
			}
			var m wsTermMsg
			if err := json.Unmarshal(msg, &m); err != nil {
				continue
			}
			switch m.Type {
			case "input":
				if _, wErr := shell.Write([]byte(m.Data)); wErr != nil {
					ws.WriteJSON(gin.H{"type": "error", "data": wErr.Error()})
					return
				}
			case "resize":
				if m.Size != nil {
					shell.Resize(m.Size.Rows, m.Size.Cols)
				}
			}
		}
	}()

	<-done
	ws.WriteJSON(gin.H{"type": "close"})
}

// ExecWS WebSocket 流式命令执行（参考 1Shell host_exec）。
// 协议（JSON）：
//   客户端 -> 服务端：{ type: "exec", command: "命令", timeout: 毫秒(可选, 默认60000, 上限600000) }
//   服务端 -> 客户端：
//     { type: "output", stream: "stdout"|"stderr", data: "..." }   // 实时回传
//     { type: "result", exitCode, durationMs, status }             // 单条命令结束
//     { type: "error", data: "..." }
//     { type: "close" }
// 一条连接可顺序发送多条 exec 消息；客户端断开即结束。权限：仅主机拥有者或管理员。
func (h *HostHandler) ExecWS(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	log := ilog.Trace(ctx)
	uid, _, isAdmin := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	host, err := h.store.GetHost(ctx, id)
	if err != nil || !canOperateHost(uid, isAdmin, host) {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "主机不存在或无权限"})
		return
	}

	ws, err := hostWsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Errorf("exec ws upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	// 权限校验失败也要在 WS 上告知（host 已存在但无权限）
	if !canOperateHost(uid, isAdmin, host) {
		ws.WriteJSON(gin.H{"type": "error", "data": "无权限操作该主机（仅主机拥有者或管理员可执行）"})
		return
	}

	// 先建 SSH 连接，复用同一条连接执行多条命令
	sshClient, err := sshx.Dial(sshx.HostConfig{
		Hostname:   host.Hostname,
		Port:       host.Port,
		Username:   host.Username,
		Password:   host.Password,
		PrivateKey: host.PrivateKey,
		Passphrase: host.Passphrase,
		Timeout:    10 * time.Second,
	})
	if err != nil {
		ws.WriteJSON(gin.H{"type": "error", "data": "连接主机失败：" + err.Error()})
		return
	}
	defer sshClient.Close()

	h.recordAudit(c, model.HostAuditHostExec, "host", host.ID, host.Name, "打开流式命令执行")

	writeWS := func(v any) {
		_ = ws.WriteJSON(v)
	}

	for {
		_, msg, rerr := ws.ReadMessage()
		if rerr != nil {
			break // 客户端断开
		}
		var req struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		}
		if jerr := json.Unmarshal(msg, &req); jerr != nil {
			writeWS(gin.H{"type": "error", "data": "消息格式错误"})
			continue
		}
		if req.Type != "exec" {
			if req.Type == "ping" {
				writeWS(gin.H{"type": "pong"})
			}
			continue
		}
		command := strings.TrimSpace(req.Command)
		if command == "" {
			writeWS(gin.H{"type": "error", "data": "command 不能为空"})
			continue
		}
		// 红线兜底：灾难性命令直接拒绝（与 exec_command 一致）
		if assessment := approval.AssessToolCall("exec_command", map[string]any{"command": command}); assessment.Block {
			writeWS(gin.H{"type": "error", "data": assessment.Reason})
			continue
		}
		timeoutMs := req.Timeout
		if timeoutMs <= 0 || timeoutMs > 600000 {
			timeoutMs = 60000
		}
		execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
		res, execErr := sshClient.ExecStream(execCtx, command, func(stream string, data []byte) {
			writeWS(gin.H{"type": "output", "stream": stream, "data": string(data)})
		})
		cancel()

		status := "success"
		if execErr != nil {
			status = "failed"
		}
		writeWS(gin.H{
			"type":      "result",
			"exitCode":  res.ExitCode,
			"durationMs": res.DurationMs,
			"status":    status,
		})
	}
}

// toJSONString 把字符串转成 JSON 字符串字面量（含转义）。
func toJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// canOperateHost 校验人工直接操作主机的权限（仅主机拥有者或管理员），用于 WebSocket 终端/命令执行。
// 智能体工具调用的授权边界是「智能体绑定的主机组」，见 service 包 ops_tools*。
func canOperateHost(uid int64, isAdmin bool, host *model.Host) bool {
	if isAdmin {
		return true
	}
	return host.OwnerID == uid
}
