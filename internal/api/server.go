package api

import (
	"chat-aggregator/internal/engine"
	"chat-aggregator/internal/storage"
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type Server struct {
	router  *gin.Engine
	db      *storage.DB
	hub     *Hub
	manager *engine.Manager
	server  *http.Server
	port    int
}

// NewServer 创建服务器实例
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

// Run 启动服务器，尝试从 basePort 开始绑定，最多尝试 maxAttempts 次
// 返回实际绑定的端口
func (s *Server) Run(basePort int) (int, error) {
	maxAttempts := 10
	actualPort := basePort

	for i := 0; i < maxAttempts; i++ {
		actualPort = basePort + i
		addr := ":" + strconv.Itoa(actualPort)

		// 先检查端口是否可用
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			fmt.Printf("端口 %d 已被占用，尝试下一个端口...\n", actualPort)
			continue
		}
		listener.Close()

		// 端口可用，创建服务器
		s.server = &http.Server{
			Addr:    addr,
			Handler: s.router,
		}
		s.port = actualPort

		// 启动服务器
		err = s.server.ListenAndServe()
		if err == http.ErrServerClosed {
			// 服务器正常关闭
			return actualPort, nil
		}
		return 0, err
	}

	return 0, fmt.Errorf("无法找到可用端口 (尝试了 %d-%d)", basePort, basePort+maxAttempts-1)
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown() {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.server.Shutdown(ctx)
	}
}

// isAddrInUse 检查错误是否是端口占用
func isAddrInUse(err error) bool {
	return err != nil && (err.Error() == "bind: address already in use" ||
		err.Error() == "listen tcp :"+strconv.Itoa(0)+": bind: address already in use")
}