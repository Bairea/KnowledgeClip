package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"chat-aggregator/internal/engine"
	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ChatRequest struct {
	Prompt  string   `json:"prompt" binding:"required"`
	SiteIDs []string `json:"site_ids"`
}

type ChatResponse struct {
	SessionID string `json:"session_id"`
}

func (s *Server) handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sites, err := storage.GetSites(s.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var targetSites []models.Site
	for _, site := range sites {
		if !site.Enabled {
			continue
		}
		if len(req.SiteIDs) > 0 {
			found := false
			for _, id := range req.SiteIDs {
				if id == site.ID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		targetSites = append(targetSites, site)
	}

	sessionID := uuid.New().String()
	if err := storage.CreateSession(s.db, sessionID, req.Prompt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, ChatResponse{SessionID: sessionID})

	var wg sync.WaitGroup
	for _, site := range targetSites {
		wg.Add(1)
		go func(site models.Site) {
			defer wg.Done()

			manager := engine.NewManager()
			defer manager.Close()

			start := time.Now()
			content, err := manager.SendMessage(context.Background(), site, req.Prompt)
			elapsed := int(time.Since(start).Milliseconds())

			msgID := uuid.New().String()
			errStr := ""
			if err != nil {
				errStr = err.Error()
				content = ""
			}

			if dbErr := storage.CreateMessage(s.db, msgID, sessionID, site.ID, content, errStr, elapsed); dbErr != nil {
				// Log but continue
			}

			update := MessageUpdate{
				Type:      "message",
				SessionID: sessionID,
				SiteID:    site.ID,
				Content:   content,
				Error:     errStr,
				ElapsedMs: elapsed,
				Done:      true,
			}
			s.hub.Broadcast(update)
		}(site)
	}

	go func() {
		wg.Wait()
		s.hub.Broadcast(MessageUpdate{
			Type:      "complete",
			SessionID: sessionID,
			Done:      true,
		})
	}()
}
