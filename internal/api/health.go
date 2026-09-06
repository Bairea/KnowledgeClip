package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"engines": s.manager.Engines(),
	})
}

// handleEngineStatus reports per-engine availability for the UI health
// badges. The bsk probe is live (daemon ping + extension check, ~2s worst
// case); everything else is construction-time state.
func (s *Server) handleEngineStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"engines": s.manager.Health(),
	})
}
