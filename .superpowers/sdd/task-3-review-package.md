# Review Package for Task 3

## Commit Range
BASE: 114f792
HEAD: 5f8bf1e3351774bf2ed26522070e0f334bd105dc

## Commit Log
5f8bf1e feat(storage): add HasSites, UpdateSelected, and selected field to all CRUD functions

## Diff Stats

 internal/api/sites.go            |  3 ++
 internal/config/config.go        |  2 +
 internal/storage/site_store.go   | 75 ++++++++++++++++++++++++++------
 internal/storage/storage_test.go | 92 ++++++++++++++++++++++++++++++++++++++++
 4 files changed, 158 insertions(+), 14 deletions(-)

## Full Diff

diff --git a/internal/api/sites.go b/internal/api/sites.go
index b469ede..0b94771 100644
--- a/internal/api/sites.go
+++ b/internal/api/sites.go
@@ -51,20 +51,21 @@ func (s *Server) handleCreateSite(c *gin.Context) {
 		return
 	}
 
 	site := models.Site{
 		ID:           req.ID,
 		Name:         req.Name,
 		URL:          req.URL,
 		EngineType:   req.EngineType,
 		Selectors:    string(selectorsJSON),
 		Enabled:      enabled,
+		Selected:     true,
 		FormatPrompt: req.FormatPrompt,
 	}
 
 	if err := storage.SaveSite(s.db, site); err != nil {
 		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
 		return
 	}
 
 	if err := s.syncConfigToYAML(); err != nil {
 		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
@@ -110,20 +111,21 @@ func (s *Server) handleUpdateSite(c *gin.Context) {
 	}
 
 	site := models.Site{
 		ID:           id,
 		Name:         req.Name,
 		URL:          req.URL,
 		EngineType:   req.EngineType,
 		Selectors:    string(selectorsJSON),
 		CookieFile:   existing.CookieFile,
 		Enabled:      enabled,
+		Selected:     existing.Selected,
 		FormatPrompt: req.FormatPrompt,
 	}
 
 	if site.Name == "" {
 		site.Name = existing.Name
 	}
 	if site.URL == "" {
 		site.URL = existing.URL
 	}
 	if site.EngineType == "" {
@@ -194,18 +196,19 @@ func (s *Server) syncConfigToYAML() error {
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
+			Selected:     site.Selected,
 			Engine:       config.EngineConfig{Primary: site.EngineType, Selectors: selectors},
 			FormatPrompt: site.FormatPrompt,
 			CookieFile:   site.CookieFile,
 		})
 	}
 
 	return config.Save(sitesConfigPath, cfg)
 }
diff --git a/internal/config/config.go b/internal/config/config.go
index 0406e91..6066381 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -18,20 +18,21 @@ type GlobalConfig struct {
 type EngineConfig struct {
 	Primary   string            `yaml:"primary"`
 	Selectors map[string]string `yaml:"selectors"`
 }
 
 type SiteConfig struct {
 	ID           string       `yaml:"id"`
 	Name         string       `yaml:"name"`
 	URL          string       `yaml:"url"`
 	Enabled      bool         `yaml:"enabled"`
+	Selected     bool         `yaml:"selected"`
 	Engine       EngineConfig `yaml:"engine"`
 	FormatPrompt string       `yaml:"format_prompt"`
 	CookieFile   string       `yaml:"cookie_file"`
 }
 
 type Config struct {
 	Global GlobalConfig `yaml:"global"`
 	Sites  []SiteConfig `yaml:"sites"`
 }
 
@@ -67,16 +68,17 @@ func (cfg *Config) ToModels() []models.Site {
 	for _, s := range cfg.Sites {
 		selectorsJSON, _ := json.Marshal(s.Engine.Selectors)
 		site := models.Site{
 			ID:           s.ID,
 			Name:         s.Name,
 			URL:          s.URL,
 			EngineType:   s.Engine.Primary,
 			Selectors:    string(selectorsJSON),
 			CookieFile:   s.CookieFile,
 			Enabled:      s.Enabled,
+			Selected:     s.Selected,
 			FormatPrompt: s.FormatPrompt,
 		}
 		result = append(result, site)
 	}
 	return result
 }
diff --git a/internal/storage/site_store.go b/internal/storage/site_store.go
index ef2b6dd..7cfbacb 100644
--- a/internal/storage/site_store.go
+++ b/internal/storage/site_store.go
@@ -8,93 +8,108 @@ import (
 
 func SyncSites(db *DB, sites []models.Site) error {
 	conn := db.Conn()
 	tx, err := conn.Begin()
 	if err != nil {
 		return fmt.Errorf("begin transaction: %w", err)
 	}
 	defer tx.Rollback()
 
 	stmt, err := tx.Prepare(`
-		INSERT INTO sites (id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt)
-		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
+		INSERT INTO sites (id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt, selected)
+		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
 		ON CONFLICT(id) DO UPDATE SET
 			name = excluded.name,
 			url = excluded.url,
 			engine_type = excluded.engine_type,
 			selectors = excluded.selectors,
 			cookie_file = excluded.cookie_file,
 			enabled = excluded.enabled,
-			format_prompt = excluded.format_prompt
+			format_prompt = excluded.format_prompt,
+			selected = excluded.selected
 	`)
 	if err != nil {
 		return fmt.Errorf("prepare upsert: %w", err)
 	}
 	defer stmt.Close()
 
 	for _, site := range sites {
 		enabled := 0
 		if site.Enabled {
 			enabled = 1
 		}
-		_, err := stmt.Exec(site.ID, site.Name, site.URL, site.EngineType, site.Selectors, site.CookieFile, enabled, site.FormatPrompt)
+		selected := 0
+		if site.Selected {
+			selected = 1
+		}
+		_, err := stmt.Exec(site.ID, site.Name, site.URL, site.EngineType, site.Selectors, site.CookieFile, enabled, site.FormatPrompt, selected)
 		if err != nil {
 			return fmt.Errorf("upsert site %s: %w", site.ID, err)
 		}
 	}
 
 	if err := tx.Commit(); err != nil {
 		return fmt.Errorf("commit transaction: %w", err)
 	}
 
 	return nil
 }
 
 func SaveSite(db *DB, site models.Site) error {
 	enabled := 0
 	if site.Enabled {
 		enabled = 1
 	}
+	selected := 0
+	if site.Selected {
+		selected = 1
+	}
 	_, err := db.Conn().Exec(`
-		INSERT INTO sites (id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt)
-		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
+		INSERT INTO sites (id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt, selected)
+		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
 		ON CONFLICT(id) DO UPDATE SET
 			name = excluded.name,
 			url = excluded.url,
 			engine_type = excluded.engine_type,
 			selectors = excluded.selectors,
 			cookie_file = excluded.cookie_file,
 			enabled = excluded.enabled,
-			format_prompt = excluded.format_prompt
-	`, site.ID, site.Name, site.URL, site.EngineType, site.Selectors, site.CookieFile, enabled, site.FormatPrompt)
+			format_prompt = excluded.format_prompt,
+			selected = excluded.selected
+	`, site.ID, site.Name, site.URL, site.EngineType, site.Selectors, site.CookieFile, enabled, site.FormatPrompt, selected)
 	if err != nil {
 		return fmt.Errorf("save site: %w", err)
 	}
 	return nil
 }
 
 func UpdateSite(db *DB, site models.Site) error {
 	enabled := 0
 	if site.Enabled {
 		enabled = 1
 	}
+	selected := 0
+	if site.Selected {
+		selected = 1
+	}
 	result, err := db.Conn().Exec(`
 		UPDATE sites SET
 			name = ?,
 			url = ?,
 			engine_type = ?,
 			selectors = ?,
 			cookie_file = ?,
 			enabled = ?,
-			format_prompt = ?
+			format_prompt = ?,
+			selected = ?
 		WHERE id = ?
-	`, site.Name, site.URL, site.EngineType, site.Selectors, site.CookieFile, enabled, site.FormatPrompt, site.ID)
+	`, site.Name, site.URL, site.EngineType, site.Selectors, site.CookieFile, enabled, site.FormatPrompt, selected, site.ID)
 	if err != nil {
 		return fmt.Errorf("update site: %w", err)
 	}
 	rows, err := result.RowsAffected()
 	if err != nil {
 		return fmt.Errorf("check rows affected: %w", err)
 	}
 	if rows == 0 {
 		return fmt.Errorf("site not found")
 	}
@@ -111,53 +126,85 @@ func DeleteSite(db *DB, id string) error {
 		return fmt.Errorf("check rows affected: %w", err)
 	}
 	if rows == 0 {
 		return fmt.Errorf("site not found")
 	}
 	return nil
 }
 
 func GetSites(db *DB) ([]models.Site, error) {
 	rows, err := db.Conn().Query(`
-		SELECT id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt, created_at
+		SELECT id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt, selected, created_at
 		FROM sites
 	`)
 	if err != nil {
 		return nil, fmt.Errorf("query sites: %w", err)
 	}
 	defer rows.Close()
 
 	var sites []models.Site = []models.Site{}
 	for rows.Next() {
 		var site models.Site
 		var enabled int
-		if err := rows.Scan(&site.ID, &site.Name, &site.URL, &site.EngineType, &site.Selectors, &site.CookieFile, &enabled, &site.FormatPrompt, &site.CreatedAt); err != nil {
+		var selected int
+		if err := rows.Scan(&site.ID, &site.Name, &site.URL, &site.EngineType, &site.Selectors, &site.CookieFile, &enabled, &site.FormatPrompt, &selected, &site.CreatedAt); err != nil {
 			return nil, fmt.Errorf("scan site: %w", err)
 		}
 		site.Enabled = enabled != 0
+		site.Selected = selected != 0
 		sites = append(sites, site)
 	}
 
 	if err := rows.Err(); err != nil {
 		return nil, fmt.Errorf("iterate sites: %w", err)
 	}
 
 	return sites, nil
 }
 
 func GetSiteByID(db *DB, id string) (*models.Site, error) {
 	var site models.Site
 	var enabled int
+	var selected int
 	row := db.Conn().QueryRow(`
-		SELECT id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt, created_at
+		SELECT id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt, selected, created_at
 		FROM sites
 		WHERE id = ?
 	`, id)
-	if err := row.Scan(&site.ID, &site.Name, &site.URL, &site.EngineType, &site.Selectors, &site.CookieFile, &enabled, &site.FormatPrompt, &site.CreatedAt); err != nil {
+	if err := row.Scan(&site.ID, &site.Name, &site.URL, &site.EngineType, &site.Selectors, &site.CookieFile, &enabled, &site.FormatPrompt, &selected, &site.CreatedAt); err != nil {
 		if err == sql.ErrNoRows {
 			return nil, nil
 		}
 		return nil, fmt.Errorf("scan site: %w", err)
 	}
 	site.Enabled = enabled != 0
+	site.Selected = selected != 0
 	return &site, nil
 }
+
+func HasSites(db *DB) (bool, error) {
+	var count int
+	err := db.Conn().QueryRow(`SELECT COUNT(*) FROM sites`).Scan(&count)
+	if err != nil {
+		return false, fmt.Errorf("count sites: %w", err)
+	}
+	return count > 0, nil
+}
+
+func UpdateSelected(db *DB, id string, selected bool) error {
+	selectedInt := 0
+	if selected {
+		selectedInt = 1
+	}
+	result, err := db.Conn().Exec(`UPDATE sites SET selected = ? WHERE id = ?`, selectedInt, id)
+	if err != nil {
+		return fmt.Errorf("update selected: %w", err)
+	}
+	rows, err := result.RowsAffected()
+	if err != nil {
+		return fmt.Errorf("check rows affected: %w", err)
+	}
+	if rows == 0 {
+		return fmt.Errorf("site not found")
+	}
+	return nil
+}
\ No newline at end of file
diff --git a/internal/storage/storage_test.go b/internal/storage/storage_test.go
index 507c7e2..7c67bef 100644
--- a/internal/storage/storage_test.go
+++ b/internal/storage/storage_test.go
@@ -235,20 +235,21 @@ func TestMessagesOrderedByTurn(t *testing.T) {
 func TestSiteCreateAndGet(t *testing.T) {
 	db := newTestDB(t)
 
 	site := models.Site{
 		ID:           "test-site",
 		Name:         "Test Site",
 		URL:          "https://example.com",
 		EngineType:   "cdp",
 		Selectors:    `{"input":"#input","submit":"#submit","answer":"#answer","wait_for":"#answer"}`,
 		Enabled:      true,
+		Selected:     true,
 		FormatPrompt: "Use markdown",
 		CreatedAt:    time.Now(),
 	}
 
 	if err := SaveSite(db, site); err != nil {
 		t.Fatalf("SaveSite failed: %v", err)
 	}
 
 	sites, err := GetSites(db)
 	if err != nil {
@@ -256,11 +257,102 @@ func TestSiteCreateAndGet(t *testing.T) {
 	}
 	if len(sites) != 1 {
 		t.Fatalf("expected 1 site, got %d", len(sites))
 	}
 	if sites[0].ID != "test-site" {
 		t.Errorf("expected site ID 'test-site', got '%s'", sites[0].ID)
 	}
 	if sites[0].FormatPrompt != "Use markdown" {
 		t.Errorf("expected format_prompt 'Use markdown', got '%s'", sites[0].FormatPrompt)
 	}
+	if !sites[0].Selected {
+		t.Error("expected site Selected=true")
+	}
+}
+
+func TestHasSites(t *testing.T) {
+	db := newTestDB(t)
+
+	hasSites, err := HasSites(db)
+	if err != nil {
+		t.Fatalf("HasSites failed: %v", err)
+	}
+	if hasSites {
+		t.Error("expected HasSites=false for empty database")
+	}
+
+	site := models.Site{
+		ID:         "test-site",
+		Name:       "Test Site",
+		URL:        "https://example.com",
+		EngineType: "cdp",
+		Enabled:    true,
+		Selected:   true,
+	}
+	if err := SaveSite(db, site); err != nil {
+		t.Fatalf("SaveSite failed: %v", err)
+	}
+
+	hasSites, err = HasSites(db)
+	if err != nil {
+		t.Fatalf("HasSites failed: %v", err)
+	}
+	if !hasSites {
+		t.Error("expected HasSites=true after adding site")
+	}
+}
+
+func TestUpdateSelected(t *testing.T) {
+	db := newTestDB(t)
+
+	site := models.Site{
+		ID:         "test-site",
+		Name:       "Test Site",
+		URL:        "https://example.com",
+		EngineType: "cdp",
+		Enabled:    true,
+		Selected:   true,
+	}
+	if err := SaveSite(db, site); err != nil {
+		t.Fatalf("SaveSite failed: %v", err)
+	}
+
+	if err := UpdateSelected(db, "test-site", false); err != nil {
+		t.Fatalf("UpdateSelected failed: %v", err)
+	}
+
+	sites, err := GetSites(db)
+	if err != nil {
+		t.Fatalf("GetSites failed: %v", err)
+	}
+	if sites[0].Selected {
+		t.Error("expected site Selected=false after update")
+	}
+
+	if err := UpdateSelected(db, "nonexistent", true); err == nil {
+		t.Error("expected error when updating nonexistent site")
+	}
+}
+
+func TestSelectedFieldPersisted(t *testing.T) {
+	db := newTestDB(t)
+
+	site := models.Site{
+		ID:         "test-site",
+		Name:       "Test Site",
+		URL:        "https://example.com",
+		EngineType: "cdp",
+		Enabled:    true,
+		Selected:   false,
+	}
+	if err := SaveSite(db, site); err != nil {
+		t.Fatalf("SaveSite failed: %v", err)
+	}
+
+	retrieved, err := GetSiteByID(db, "test-site")
+	if err != nil {
+		t.Fatalf("GetSiteByID failed: %v", err)
+	}
+	if retrieved.Selected {
+		t.Error("expected Selected=false to be persisted")
+	}
 }
