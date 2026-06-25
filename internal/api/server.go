package api

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func NewServer() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.Run(":8080")
}
