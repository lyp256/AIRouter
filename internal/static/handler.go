// Package static 提供静态文件服务支持
package static

import (
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lyp256/airouter/web"
)

// Handler 静态文件处理器
type Handler struct {
	distFS    http.FileSystem
	indexHTML []byte
}

// NewHandler 创建静态文件处理器
func NewHandler() (*Handler, error) {
	// 从 embed.FS 中获取 dist 子目录
	subFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		return nil, err
	}

	// 预加载 index.html
	indexHTML, err := fs.ReadFile(subFS, "index.html")
	if err != nil {
		return nil, err
	}

	return &Handler{
		distFS:    http.FS(subFS),
		indexHTML: indexHTML,
	}, nil
}

// ServeStatic 提供静态文件服务
func (h *Handler) ServeStatic(c *gin.Context) {
	path := c.Param("filepath")

	// 如果没有 filepath 参数，从请求路径获取（如 /favicon.svg）
	if path == "" || path == "/" {
		path = c.Request.URL.Path
	}
	path = strings.TrimPrefix(path, "/")

	// 如果路径不以 assets/ 开头，且不是根目录下的静态文件
	// 说明是从 /assets/*filepath 路由来的，需要添加 assets/ 前缀
	if !strings.HasPrefix(path, "assets/") && path != "favicon.svg" && path != "icons.svg" {
		path = "assets/" + path
	}

	// 打开文件
	f, err := h.distFS.Open(path)
	if err != nil {
		h.ServeIndexHTML(c)
		return
	}
	defer f.Close()

	// 获取文件信息
	stat, err := f.Stat()
	if err != nil {
		h.ServeIndexHTML(c)
		return
	}

	// 如果是目录，返回 index.html
	if stat.IsDir() {
		h.ServeIndexHTML(c)
		return
	}

	// 设置缓存头
	// 带哈希的资源文件（如 index-xxxx.js）可长期缓存
	if strings.HasPrefix(path, "assets/") {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		c.Header("Cache-Control", "public, max-age=3600")
	}
	c.Header("Vary", "Accept-Encoding")

	// 设置 Content-Type
	contentType := "application/octet-stream"
	switch filepath.Ext(path) {
	case ".js":
		contentType = "application/javascript; charset=utf-8"
	case ".css":
		contentType = "text/css; charset=utf-8"
	case ".svg":
		contentType = "image/svg+xml"
	case ".html":
		contentType = "text/html; charset=utf-8"
	case ".json":
		contentType = "application/json; charset=utf-8"
	}
	c.Header("Content-Type", contentType)

	// 使用 http.ServeContent 处理范围请求（如果实现了 ReadSeeker）
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(c.Writer, c.Request, filepath.Base(path), stat.ModTime(), rs)
	} else {
		// 降级处理：直接复制内容
		c.DataFromReader(http.StatusOK, stat.Size(), contentType, f, nil)
	}
}

// ServeIndexHTML 返回 index.html（SPA fallback）
func (h *Handler) ServeIndexHTML(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Data(http.StatusOK, "text/html; charset=utf-8", h.indexHTML)
}

// IsAPIPath 判断是否为 API 路径
func IsAPIPath(path string) bool {
	return strings.HasPrefix(path, "/v1/") ||
		strings.HasPrefix(path, "/v1") ||
		strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/health")
}
