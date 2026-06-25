package api

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed all:static
var staticFiles embed.FS

func setupStatic(r *gin.Engine) {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))

	r.NoRoute(func(c *gin.Context) {
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}
