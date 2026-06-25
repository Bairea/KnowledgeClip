package api

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type MessageUpdate struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	SiteID    string `json:"site_id,omitempty"`
	Content   string `json:"content,omitempty"`
	Error     string `json:"error,omitempty"`
	ElapsedMs int    `json:"elapsed_ms,omitempty"`
	Done      bool   `json:"done"`
}

type Hub struct {
	clients map[*websocket.Conn]bool
	mu      sync.RWMutex
	upgrader websocket.Upgrader
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (h *Hub) Add(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = true
}

func (h *Hub) Remove(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
	conn.Close()
}

func (h *Hub) Broadcast(msg MessageUpdate) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for conn := range h.clients {
		if err := conn.WriteJSON(msg); err != nil {
			conn.Close()
			delete(h.clients, conn)
		}
	}
}

func (h *Hub) handleWebSocket(c *gin.Context) {
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	h.Add(conn)

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			h.Remove(conn)
			return
		}
	}
}
