package api

import (
	"chat-aggregator/internal/config"
	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

const sitesConfigPath = "configs/sites.yaml"

type CreateSiteRequest struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	URL          string            `json:"url"`
	EngineType   string            `json:"engine_type"`
	Selectors    map[string]string `json:"selectors"`
	FormatPrompt string            `json:"format_prompt"`
}

func (s *Server) handleGetSites(c *gin.Context) {
	sites, err := storage.GetSites(s.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sites)
}

func (s *Server) handleCreateSite(c *gin.Context) {
	var req CreateSiteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.ID == "" || req.Name == "" || req.URL == "" || req.EngineType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id, name, url and engine_type are required"})
		return
	}

	selectorsJSON, err := json.Marshal(req.Selectors)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	site := models.Site{
		ID:           req.ID,
		Name:         req.Name,
		URL:          req.URL,
		EngineType:   req.EngineType,
		Selectors:    string(selectorsJSON),
		Enabled:      true,
		FormatPrompt: req.FormatPrompt,
	}

	if err := storage.SaveSite(s.db, site); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := s.syncConfigToYAML(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, site)
}

func (s *Server) handleUpdateSite(c *gin.Context) {
	id := c.Param("id")
	var req CreateSiteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := storage.GetSiteByID(s.db, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
		return
	}

	var selectorsJSON []byte
	if req.Selectors != nil {
		selectorsJSON, err = json.Marshal(req.Selectors)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		selectorsJSON = []byte(existing.Selectors)
	}

	site := models.Site{
		ID:           id,
		Name:         req.Name,
		URL:          req.URL,
		EngineType:   req.EngineType,
		Selectors:    string(selectorsJSON),
		CookieFile:   existing.CookieFile,
		Enabled:      existing.Enabled,
		FormatPrompt: req.FormatPrompt,
	}

	if site.Name == "" {
		site.Name = existing.Name
	}
	if site.URL == "" {
		site.URL = existing.URL
	}
	if site.EngineType == "" {
		site.EngineType = existing.EngineType
	}
	if site.FormatPrompt == "" {
		site.FormatPrompt = existing.FormatPrompt
	}

	if err := storage.UpdateSite(s.db, site); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := s.syncConfigToYAML(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, site)
}

func (s *Server) handleDeleteSite(c *gin.Context) {
	id := c.Param("id")
	if err := storage.DeleteSite(s.db, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := s.syncConfigToYAML(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (s *Server) syncConfigToYAML() error {
	sites, err := storage.GetSites(s.db)
	if err != nil {
		return err
	}

	cfg, err := config.Load(sitesConfigPath)
	if err != nil {
		cfg = &config.Config{}
	}

	cfg.Sites = nil
	for _, site := range sites {
		var selectors map[string]string
		if site.Selectors != "" {
			if err := json.Unmarshal([]byte(site.Selectors), &selectors); err != nil {
				selectors = make(map[string]string)
			}
		}
		cfg.Sites = append(cfg.Sites, config.SiteConfig{
			ID:           site.ID,
			Name:         site.Name,
			URL:          site.URL,
			Enabled:      site.Enabled,
			Engine:       config.EngineConfig{Primary: site.EngineType, Selectors: selectors},
			FormatPrompt: site.FormatPrompt,
			CookieFile:   site.CookieFile,
		})
	}

	return config.Save(sitesConfigPath, cfg)
}
