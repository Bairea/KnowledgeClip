package api

import (
	"chat-aggregator/internal/engine"
	"chat-aggregator/internal/storage"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Server struct {
	router  *gin.Engine
	db      *storage.DB
	hub     *Hub
	manager *engine.Manager
}

func NewServer(db *storage.DB, manager *engine.Manager) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	s := &Server{router: r, db: db, hub: NewHub(), manager: manager}
	s.setupRoutes()

	return s
}

func (s *Server) setupRoutes() {
	s.router.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	s.router.GET("/api/sites", s.handleGetSites)
	s.router.POST("/api/sites", s.handleCreateSite)
	s.router.PUT("/api/sites/:id", s.handleUpdateSite)
	s.router.DELETE("/api/sites/:id", s.handleDeleteSite)
	s.router.POST("/api/chat", s.handleChat)
	s.router.POST("/api/messages/kept", s.handleUpdateKept)
	s.router.GET("/api/export", s.handleExport)
	s.router.GET("/ws", s.hub.handleWebSocket)
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}
