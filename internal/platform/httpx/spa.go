// Package httpx — общие HTTP-хелперы для Gin.
package httpx

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// SPA отдаёт собранный React из fsys, а для неизвестных путей — index.html,
// чтобы работала клиентская маршрутизация. Пути /api и /tg не перехватываются.
func SPA(fsys fs.FS) gin.HandlerFunc {
	fileServer := http.FileServer(http.FS(fsys))
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/tg/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if name := strings.TrimPrefix(p, "/"); name != "" {
			if _, err := fs.Stat(fsys, name); err == nil {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			c.String(http.StatusNotFound, "frontend is not built: run `make web-build` or use the Vite dev server")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	}
}
