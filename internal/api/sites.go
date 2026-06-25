package api

import (
	"chat-aggregator/internal/storage"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleGetSites(c *gin.Context) {
	sites, err := storage.GetSites(s.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sites)
}
