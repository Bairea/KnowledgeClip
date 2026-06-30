package api

import (
	"chat-aggregator/internal/engine"
	"chat-aggregator/internal/storage"

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
	setupStatic(r)

	return s
}

func (s *Server) setupRoutes() {
	s.router.GET("/api/health", s.handleHealth)
	s.router.GET("/api/sites", s.handleGetSites)
	s.router.POST("/api/sites", s.handleCreateSite)
	s.router.PUT("/api/sites/:id", s.handleUpdateSite)
	s.router.DELETE("/api/sites/:id", s.handleDeleteSite)
	s.router.GET("/api/detect", s.handleDetectSelectors)
	s.router.POST("/api/chat", s.handleChat)
	s.router.GET("/api/sessions", s.handleGetSessions)
	s.router.DELETE("/api/sessions/:id", s.handleDeleteSession)
	s.router.GET("/api/sessions/:id/messages", s.handleGetSessionMessages)
	s.router.POST("/api/messages/kept", s.handleUpdateKept)
	s.router.GET("/api/export", s.handleExport)
	s.router.GET("/ws", s.hub.handleWebSocket)
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}
