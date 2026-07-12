# Review Package for Task 5

## Commit Range
BASE: 5f8bf1e
HEAD: 545d99a620a1fbf81ce821bfb6040fa8b7cea128

## Commit Log
545d99a feat(api): add PUT /api/sites/:id/selected endpoint

## Diff Stats

 internal/api/server.go |  1 +
 internal/api/sites.go  | 25 +++++++++++++++++++++++++
 2 files changed, 26 insertions(+)

## Full Diff

diff --git a/internal/api/server.go b/internal/api/server.go
index db87c6a..993288b 100644
--- a/internal/api/server.go
+++ b/internal/api/server.go
@@ -32,20 +32,21 @@ func NewServer(db *storage.DB, manager *engine.Manager) *Server {
 	setupStatic(r)
 
 	return s
 }
 
 func (s *Server) setupRoutes() {
 	s.router.GET("/api/health", s.handleHealth)
 	s.router.GET("/api/sites", s.handleGetSites)
 	s.router.POST("/api/sites", s.handleCreateSite)
 	s.router.PUT("/api/sites/:id", s.handleUpdateSite)
+	s.router.PUT("/api/sites/:id/selected", s.handleUpdateSelected)
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
diff --git a/internal/api/sites.go b/internal/api/sites.go
index 0b94771..a799aa6 100644
--- a/internal/api/sites.go
+++ b/internal/api/sites.go
@@ -6,20 +6,24 @@ import (
 	"chat-aggregator/internal/models"
 	"chat-aggregator/internal/storage"
 	"encoding/json"
 	"net/http"
 
 	"github.com/gin-gonic/gin"
 )
 
 const sitesConfigPath = "configs/sites.yaml"
 
+type UpdateSelectedRequest struct {
+	Selected bool `json:"selected"`
+}
+
 type CreateSiteRequest struct {
 	ID           string            `json:"id"`
 	Name         string            `json:"name"`
 	URL          string            `json:"url"`
 	EngineType   string            `json:"engine_type"`
 	Selectors    map[string]string `json:"selectors"`
 	FormatPrompt string            `json:"format_prompt"`
 }
 
 func (s *Server) handleGetSites(c *gin.Context) {
@@ -156,20 +160,41 @@ func (s *Server) handleDeleteSite(c *gin.Context) {
 	}
 
 	if err := s.syncConfigToYAML(); err != nil {
 		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
 		return
 	}
 
 	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
 }
 
+func (s *Server) handleUpdateSelected(c *gin.Context) {
+	id := c.Param("id")
+	var req UpdateSelectedRequest
+	if err := c.ShouldBindJSON(&req); err != nil {
+		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
+		return
+	}
+
+	if err := storage.UpdateSelected(s.db, id, req.Selected); err != nil {
+		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
+		return
+	}
+
+	if err := s.syncConfigToYAML(); err != nil {
+		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
+		return
+	}
+
+	c.JSON(http.StatusOK, gin.H{"message": "updated"})
+}
+
 func (s *Server) handleDetectSelectors(c *gin.Context) {
 	url := c.Query("url")
 	if url == "" {
 		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
 		return
 	}
 
 	result, err := engine.DetectSelectors(url)
 	if err != nil {
 		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
