package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"aiagent/pkg/app/config"
	"aiagent/pkg/ilog"

	"github.com/gin-gonic/gin"
)

// attachStatic 挂载前端静态文件服务。
func attachStatic(engine *gin.Engine, conf *config.Config) {
	distDir := filepath.Join("..", "web", "dist")
	if _, err := os.Stat(distDir); os.IsNotExist(err) {
		distDir = "web/dist"
	}
	if _, err := os.Stat(distDir); os.IsNotExist(err) {
		ilog.Infof("frontend dist not found, serving API only")
		return
	}

	engine.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/metrics") || strings.HasPrefix(path, "/output") {
			c.Next()
			return
		}

		filePath := filepath.Join(distDir, path)
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			http.ServeFile(c.Writer, c.Request, filePath)
			c.Abort()
			return
		}

		indexPath := filepath.Join(distDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(c.Writer, c.Request, indexPath)
			c.Abort()
			return
		}

		c.Next()
	})
}

// attachOutputStatic 挂载平台本地产物目录（/output/*）的静态访问。
// 与 attachStatic 的前端 dist 互不干扰：输出根目录由 outputDir() 决定（env AIA_OUTPUT_DIR，默认 data/output）。
func attachOutputStatic(engine *gin.Engine) {
	root := outputDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		ilog.Warnf("mkdir output dir %s: %v", root, err)
	}
	engine.GET("/output/*filepath", func(c *gin.Context) {
		rel := c.Param("filepath") // 形如 /amap.html
		if rel == "" || rel == "/" {
			c.Status(http.StatusNotFound)
			return
		}
		clean := filepath.Clean(rel)
		if strings.HasPrefix(clean, "..") {
			c.Status(http.StatusForbidden)
			return
		}
		fp := filepath.Join(root, clean)
		absRoot, _ := filepath.Abs(root)
		absFp, _ := filepath.Abs(fp)
		if absFp != absRoot && !strings.HasPrefix(absFp, absRoot+string(os.PathSeparator)) {
			c.Status(http.StatusForbidden)
			return
		}
		c.File(fp)
	})
}

// outputDir 返回平台本地产物根目录。
func outputDir() string {
	if d := os.Getenv("AIA_OUTPUT_DIR"); d != "" {
		return d
	}
	return "data/output"
}