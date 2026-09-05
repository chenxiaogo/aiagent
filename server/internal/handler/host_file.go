package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/pkg/ilog"
	"aiagent/pkg/sshx"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// ---------- 主机文件管理（SFTP 之外的轻量方案：SSH exec + 流式读写）----------
//
// 列目录走 stat，下载走 cat、上传走 cat >，全程二进制安全且支持大文件流式传输，
// 不需要额外引入 SFTP 依赖。所有操作都要求操作者是主机拥有者（或管理员），并写审计。

// loadHostForFile 取出主机并校验操作权限，失败时已写好响应并返回 nil。
func (h *HostHandler) loadHostForFile(c *gin.Context) *model.Host {
	uid, _, _ := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	host, err := h.store.GetHost(tracex.FromRequest(c), id)
	if err != nil || host == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "主机不存在"})
		return nil
	}
	// 与终端、命令记录保持一致：拥有者本人或有主机管理权限的管理员可操作
	if host.OwnerID != uid && !middleware.CurrentIsAdmin(c) {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "主机不存在"})
		return nil
	}
	return host
}

// dialHost 按主机凭据建立 SSH 连接。
func (h *HostHandler) dialHost(host *model.Host) (*sshx.Client, error) {
	return sshx.Dial(sshx.HostConfig{
		Hostname:   host.Hostname,
		Port:       host.Port,
		Username:   host.Username,
		Password:   host.Password,
		PrivateKey: host.PrivateKey,
		Passphrase: host.Passphrase,
		Timeout:    15 * time.Second,
	})
}

// safeRemotePath 归一化远端路径：必须是绝对路径，且不含换行等会破坏 shell 命令的字符。
// 真正的命令注入由 sshx.escapePath 的单引号包裹兜底，这里只挡掉明显非法的输入。
func safeRemotePath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "/", nil
	}
	if strings.ContainsAny(p, "\n\r") {
		return "", fmt.Errorf("路径不能包含换行符")
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	// 去掉 .. 段，避免路径穿越到意料之外的位置
	cleaned := path.Clean(p)
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("非法路径")
	}
	return cleaned, nil
}

// ListFiles 列出远程目录。
func (h *HostHandler) ListFiles(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	host := h.loadHostForFile(c)
	if host == nil {
		return
	}
	client, err := h.dialHost(host)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 5, "message": "连接主机失败：" + err.Error()})
		return
	}
	defer client.Close()

	// 未指定路径时以账号家目录为起点：非 root 账号访问 / 会直接报权限错误
	wanted := strings.TrimSpace(c.Query("path"))
	dir := wanted
	if dir == "" {
		home, homeErr := client.HomeDir(ctx)
		if homeErr != nil || home == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "无法获取账号家目录，请指定路径"})
			return
		}
		dir = home
	}
	dir, err = safeRemotePath(dir)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": err.Error()})
		return
	}

	entries, err := client.ListDir(ctx, dir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "读取目录失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"path": dir, "entries": entries},
	})
}

// DownloadFile 下载远程文件（流式，二进制安全）。
func (h *HostHandler) DownloadFile(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	host := h.loadHostForFile(c)
	if host == nil {
		return
	}
	filePath, err := safeRemotePath(c.Query("path"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": err.Error()})
		return
	}
	if filePath == "/" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "请指定要下载的文件"})
		return
	}

	client, err := h.dialHost(host)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 5, "message": "连接主机失败：" + err.Error()})
		return
	}
	defer client.Close()

	reader, cleanup, err := client.DownloadStream(ctx, filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "打开文件失败：" + err.Error()})
		return
	}
	defer cleanup()

	h.recordAudit(c, model.HostAuditFileDownload, "host", host.ID, host.Name, "下载文件 "+filePath)

	fileName := path.Base(filePath)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.QueryEscape(fileName)))
	c.Header("Content-Type", "application/octet-stream")
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, reader); err != nil {
		ilog.Warnf("download %s from host %d: %v", filePath, host.ID, err)
	}
}

// UploadFile 上传文件到远程主机（multipart/form-data：file + path）。
// 先落到 .uploading 临时文件再改名，避免中途失败在目标路径留下半截文件。
func (h *HostHandler) UploadFile(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	host := h.loadHostForFile(c)
	if host == nil {
		return
	}
	dir, err := safeRemotePath(c.PostForm("path"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": err.Error()})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "请选择要上传的文件"})
		return
	}
	defer file.Close()

	client, err := h.dialHost(host)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 5, "message": "连接主机失败：" + err.Error()})
		return
	}
	defer client.Close()

	target := path.Join(dir, path.Base(header.Filename))
	temp := target + ".uploading"

	writer, finish, err := client.UploadStream(ctx, temp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "创建远端文件失败：" + err.Error()})
		return
	}
	written, copyErr := io.Copy(writer, file)
	if finishErr := finish(); finishErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": finishErr.Error()})
		return
	}
	if copyErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "上传中断：" + copyErr.Error()})
		return
	}

	// 改名落位：临时文件 -> 目标文件
	if err := client.ExecChecked(ctx, fmt.Sprintf("mv -f %s %s", sshx.QuotePath(temp), sshx.QuotePath(target))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "写入目标文件失败：" + err.Error()})
		return
	}

	h.recordAudit(c, model.HostAuditFileUpload, "host", host.ID, host.Name,
		fmt.Sprintf("上传文件 %s（%d 字节）", target, written))

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"path": target, "size": written, "name": header.Filename},
	})
}

// MkdirRemote 在远程主机上创建目录。
func (h *HostHandler) MkdirRemote(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	host := h.loadHostForFile(c)
	if host == nil {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "目录路径不能为空"})
		return
	}
	dir, err := safeRemotePath(req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": err.Error()})
		return
	}

	client, err := h.dialHost(host)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 5, "message": "连接主机失败：" + err.Error()})
		return
	}
	defer client.Close()

	if err := client.ExecChecked(ctx, fmt.Sprintf("mkdir -p %s", sshx.QuotePath(dir))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "创建目录失败：" + err.Error()})
		return
	}
	h.recordAudit(c, model.HostAuditFileMkdir, "host", host.ID, host.Name, "创建目录 "+dir)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"path": dir}})
}

// DeleteRemoteFile 删除远程文件（只允许文件，目录请用 rmdir 语义，避免误删整棵目录）。
func (h *HostHandler) DeleteRemoteFile(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	host := h.loadHostForFile(c)
	if host == nil {
		return
	}
	filePath, err := safeRemotePath(c.Query("path"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": err.Error()})
		return
	}
	if filePath == "/" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "不能删除根目录"})
		return
	}
	// 目录只允许删空目录（rmdir），避免 rm -rf 造成的不可逆损失
	isDir := c.Query("type") == "dir"
	cmd := fmt.Sprintf("rm -f %s", sshx.QuotePath(filePath))
	if isDir {
		cmd = fmt.Sprintf("rmdir %s", sshx.QuotePath(filePath))
	}

	client, err := h.dialHost(host)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 5, "message": "连接主机失败：" + err.Error()})
		return
	}
	defer client.Close()

	if err := client.ExecChecked(ctx, cmd); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "删除失败：" + err.Error()})
		return
	}
	h.recordAudit(c, model.HostAuditFileDelete, "host", host.ID, host.Name, "删除 "+filePath)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"path": filePath}})
}

// RenameRemoteFile 重命名远程文件 / 目录。
func (h *HostHandler) RenameRemoteFile(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	host := h.loadHostForFile(c)
	if host == nil {
		return
	}
	var req struct {
		Path    string `json:"path"`
		NewName string `json:"newName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Path) == "" || strings.TrimSpace(req.NewName) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数不完整"})
		return
	}
	// 新名字只允许纯文件名，禁止夹带路径分隔符，防止把文件挪到目录之外
	newName := path.Base(strings.TrimSpace(req.NewName))
	if newName == "." || newName == "/" || strings.ContainsAny(newName, "/\\") {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "新名称不合法"})
		return
	}

	client, err := h.dialHost(host)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 5, "message": "连接主机失败：" + err.Error()})
		return
	}
	defer client.Close()

	target := path.Join(path.Dir(path.Clean(req.Path)), newName)
	if err := client.ExecChecked(ctx, fmt.Sprintf("mv -f %s %s", sshx.QuotePath(path.Clean(req.Path)), sshx.QuotePath(target))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "重命名失败：" + err.Error()})
		return
	}
	h.recordAudit(c, model.HostAuditFileRename, "host", host.ID, host.Name,
		fmt.Sprintf("重命名 %s -> %s", req.Path, newName))
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"path": target}})
}
