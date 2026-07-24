package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ChatRequest struct {
	Prompt    string   `json:"prompt" binding:"required"`
	SiteIDs   []string `json:"site_ids"`
	SessionID string   `json:"session_id"`
	Turn      int      `json:"turn"`
}

type ChatResponse struct {
	SessionID string `json:"session_id"`
}

type UpdateKeptRequest struct {
	MessageID string `json:"message_id" binding:"required"`
	Kept      bool   `json:"kept"`
}

func (s *Server) handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[chat] request: prompt=%q site_ids=%v session_id=%s",
		req.Prompt[:min(50, len(req.Prompt))], req.SiteIDs, req.SessionID)

	sites, err := storage.GetSites(s.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var targetSites []models.Site
	for _, site := range sites {
		if !site.Enabled {
			if len(req.SiteIDs) > 0 {
				for _, id := range req.SiteIDs {
					if id == site.ID {
						log.Printf("chat: skip disabled site id=%s (requested by client)", site.ID)
					}
				}
			}
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

	sessionID := req.SessionID
	isNewSession := sessionID == ""
	if isNewSession {
		sessionID = uuid.New().String()
		if err := storage.CreateSession(s.db, sessionID, req.Prompt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, ChatResponse{SessionID: sessionID})

	go func() {
		batchStart := time.Now()
		s.manager.SendToSites(context.Background(), targetSites, req.Prompt, isNewSession, func(site models.Site, content string, err error) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[chat] onResult panic: site=%s err=%v", site.ID, r)
					s.hub.Broadcast(MessageUpdate{
						Type:      "message",
						SessionID: sessionID,
						SiteID:    site.ID,
						Error:     fmt.Sprintf("internal panic: %v", r),
						Done:      true,
					})
				}
			}()

			elapsed := int(time.Since(batchStart).Milliseconds())
			log.Printf("[chat] site=%s done in %dms err=%v", site.ID, elapsed, err)

			msgID := uuid.New().String()
			errStr := ""
			if err != nil {
				errStr = err.Error()
				content = ""
			}

			if dbErr := storage.CreateMessage(s.db, msgID, sessionID, site.ID, content, errStr, elapsed, req.Turn, req.Prompt); dbErr != nil {
				log.Printf("create message: site=%s err=%v", site.ID, dbErr)
			}

			s.hub.Broadcast(MessageUpdate{
				Type:      "message",
				SessionID: sessionID,
				MessageID: msgID,
				SiteID:    site.ID,
				Content:   content,
				Error:     errStr,
				ElapsedMs: elapsed,
				Done:      true,
			})
		})
		s.hub.Broadcast(MessageUpdate{
			Type:      "complete",
			SessionID: sessionID,
			Done:      true,
		})
	}()
}

func (s *Server) handleDeleteSession(c *gin.Context) {
	sessionID := c.Param("id")
	if err := storage.DeleteSession(s.db, sessionID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) handleGetSessions(c *gin.Context) {
	sessions, err := storage.GetSessions(s.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sessions)
}

func (s *Server) handleGetSessionMessages(c *gin.Context) {
	sessionID := c.Param("id")
	messages, err := storage.GetMessagesBySession(s.db, sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, messages)
}

func (s *Server) handleUpdateKept(c *gin.Context) {
	var req UpdateKeptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := storage.UpdateMessageKept(s.db, req.MessageID, req.Kept); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
