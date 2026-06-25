package api

import (
	"chat-aggregator/internal/storage"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Server struct {
	router *gin.Engine
	db     *storage.DB
	hub    *Hub
}

func NewServer(db *storage.DB) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	s := &Server{router: r, db: db, hub: NewHub()}
	s.setupRoutes()

	return s
}

func (s *Server) setupRoutes() {
	s.router.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	s.router.GET("/api/sites", s.handleGetSites)
	s.router.POST("/api/chat", s.handleChat)
	s.router.POST("/api/messages/kept", s.handleUpdateKept)
	s.router.GET("/ws", s.hub.handleWebSocket)
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}
