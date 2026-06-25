package api

import (
	"chat-aggregator/internal/storage"
	"net/http"

	"github.com/gin-gonic/gin"
)

func NewServer(db *storage.DB) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.Run(":8080")
}
