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
	MessageID string `json:"message_id,omitempty"`
	SiteID    string `json:"site_id,omitempty"`
	Content   string `json:"content,omitempty"`
	Error     string `json:"error,omitempty"`
	ElapsedMs int    `json:"elapsed_ms,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Done      bool   `json:"done"`
}

type clientEntry struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

type Hub struct {
	clients  map[*websocket.Conn]*clientEntry
	mu       sync.RWMutex
	upgrader websocket.Upgrader
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*websocket.Conn]*clientEntry),
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
	h.clients[conn] = &clientEntry{conn: conn}
}

func (h *Hub) Remove(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[conn]; ok {
		conn.Close()
		delete(h.clients, conn)
	}
}

func (h *Hub) Broadcast(msg MessageUpdate) {
	h.mu.RLock()
	entries := make([]*clientEntry, 0, len(h.clients))
	for _, entry := range h.clients {
		entries = append(entries, entry)
	}
	h.mu.RUnlock()

	var failed []*clientEntry
	for _, entry := range entries {
		entry.mu.Lock()
		err := entry.conn.WriteJSON(msg)
		entry.mu.Unlock()
		if err != nil {
			failed = append(failed, entry)
		}
	}

	if len(failed) > 0 {
		h.mu.Lock()
		for _, entry := range failed {
			if _, ok := h.clients[entry.conn]; ok {
				entry.conn.Close()
				delete(h.clients, entry.conn)
			}
		}
		h.mu.Unlock()
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
