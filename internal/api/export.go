package api

import (
	"net/http"
	"strings"
	"time"

	"chat-aggregator/internal/export"
	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleExport(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
		return
	}

	format := c.Query("format")
	if format == "" {
		format = "json"
	}
	format = strings.ToLower(format)
	if format != "json" && format != "markdown" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "format must be json or markdown"})
		return
	}

	filterKept := true
	if c.Query("filter_kept") == "false" {
		filterKept = false
	}

	session, err := storage.GetSessionByID(s.db, sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	messages, err := storage.GetMessagesBySession(s.db, sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if filterKept {
		var filtered []models.Message
		for _, msg := range messages {
			if msg.Kept {
				filtered = append(filtered, msg)
			}
		}
		messages = filtered
	}

	if format == "json" {
		data, err := export.ToJSON(*session, messages)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		timestamp := time.Now().Format("20060102_150405")
		filename := "export_" + timestamp + ".json"
		c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
		c.Data(http.StatusOK, "application/json", data)
		return
	}

	sites, err := storage.GetSites(s.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	md := export.ToMarkdown(*session, messages, sites)

	timestamp := time.Now().Format("20060102_150405")
	filename := "export_" + timestamp + ".md"
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Header("Content-Type", "text/markdown; charset=utf-8")
	c.String(http.StatusOK, md)
}
