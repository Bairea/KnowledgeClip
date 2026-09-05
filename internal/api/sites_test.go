package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"chat-aggregator/internal/config"
	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"

	"github.com/gin-gonic/gin"
)

// newTestServer builds a Server backed by an in-memory DB and the given
// sites.yaml write path.
func newTestServer(t *testing.T, configPath string) *Server {
	t.Helper()
	db, err := storage.NewDB(":memory:")
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	s := NewServer(db, nil, configPath)
	return s
}

// Regression test for the sites.yaml path bug: syncConfigToYAML used a
// hardcoded relative path ("configs/sites.yaml"), which resolves against the
// process CWD — different from the baseDir-relative path main.go uses. In dev
// (go run) every site write-back 500'd with "write config file: no such file
// or directory", so site checkboxes could never be toggled.
//
// The handler must write through the injected config path: if anyone
// reintroduces a CWD-relative path, the yaml is not created at the injected
// path and this test fails.
func TestHandleUpdateSelectedWritesInjectedConfigPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfgPath := filepath.Join(t.TempDir(), "configs", "sites.yaml")
	s := newTestServer(t, cfgPath)

	// Seed one site (selected=false), matching the app's startup sync.
	site := models.Site{
		ID:         "qwen",
		Name:       "Qwen",
		URL:        "https://www.qianwen.com/",
		EngineType: "cdp",
		Enabled:    true,
		Selected:   false,
	}
	if err := storage.SyncSites(s.db, []models.Site{site}); err != nil {
		t.Fatalf("seed site: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "qwen"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/api/sites/qwen/selected", strings.NewReader(`{"selected":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	s.handleUpdateSelected(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The yaml must exist at the injected path and carry the new selection.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("sites.yaml not written at injected path %s: %v", cfgPath, err)
	}
	var found *config.SiteConfig
	for i := range cfg.Sites {
		if cfg.Sites[i].ID == "qwen" {
			found = &cfg.Sites[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("qwen missing from written yaml")
	}
	if !found.Selected {
		t.Errorf("expected qwen selected=true in yaml, got false")
	}

	// The database must agree.
	dbSite, err := storage.GetSiteByID(s.db, "qwen")
	if err != nil {
		t.Fatalf("get site from db: %v", err)
	}
	if dbSite == nil || !dbSite.Selected {
		t.Errorf("expected qwen selected=true in db, got %+v", dbSite)
	}
}