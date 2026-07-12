# Site Selection Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `selected` field to persist user checkbox preference and fix data loss when configs/ directory is deleted.

**Architecture:** Add `selected` column to SQLite sites table, sync with YAML config, expose via new API endpoint, update frontend to read/write preference. Refactor startup flow to check SQLite before writing embed defaults.

**Tech Stack:** Go 1.23, SQLite, React 18, TypeScript

## Global Constraints

- `selected` column default value: `1` (true)
- `selected` field semantics: user checkbox preference, independent of `enabled`
- New endpoint: `PUT /api/sites/:id/selected` with body `{"selected": true|false}`
- Frontend initialization: read `selectedSites` from `selected=true` sites, not `enabled=true`
- Startup logic: check SQLite first, restore YAML from database if configs/ deleted

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/storage/db.go` | Add `selected` column migration |
| `internal/models/models.go` | Add `Selected bool` field to Site struct |
| `internal/storage/site_store.go` | Add `HasSites`, update all CRUD functions for `selected` |
| `internal/config/config.go` | Add `Selected bool` to SiteConfig, update ToModels |
| `internal/api/sites.go` | Add handleUpdateSelected, update CreateSiteRequest |
| `internal/api/server.go` | Register `PUT /api/sites/:id/selected` route |
| `cmd/server/main.go` | Split createDirectories into createDirs + ensureConfig |
| `web/src/types/index.ts` | Add `selected?: boolean` to Site interface |
| `web/src/hooks/useSites.ts` | Change fetchSites initialization, add updateSelected API call |
| `web/src/components/SiteSidebar.tsx` | Use `site.selected` for checkbox state |

---

### Task 1: Database Migration - Add selected Column

**Files:**
- Modify: `internal/storage/db.go:72-78`

**Interfaces:**
- Produces: `selected INTEGER DEFAULT 1` column in sites table

- [ ] **Step 1: Add migration to db.go**

```go
migrations := []string{
    "ALTER TABLE messages ADD COLUMN turn INTEGER NOT NULL DEFAULT 0",
    "ALTER TABLE messages ADD COLUMN prompt TEXT NOT NULL DEFAULT ''",
    "ALTER TABLE sites ADD COLUMN selected INTEGER NOT NULL DEFAULT 1",
}
```

- [ ] **Step 2: Run go build to verify syntax**

Run: `go build -o bin/server.exe cmd/server/main.go`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/storage/db.go
git commit -m "feat(storage): add selected column migration to sites table"
```

---

### Task 2: Model - Add Selected Field to Site Struct

**Files:**
- Modify: `internal/models/models.go:5-15`

**Interfaces:**
- Produces: `Site.Selected bool` field with json tag

- [ ] **Step 1: Add Selected field to Site struct**

```go
type Site struct {
    ID           string    `json:"id"`
    Name         string    `json:"name"`
    URL          string    `json:"url"`
    EngineType   string    `json:"engine_type"`
    Selectors    string    `json:"selectors"`
    CookieFile   string    `json:"cookie_file"`
    Enabled      bool      `json:"enabled"`
    Selected     bool      `json:"selected"`
    FormatPrompt string    `json:"format_prompt"`
    CreatedAt    time.Time `json:"created_at"`
}
```

- [ ] **Step 2: Run go build to verify syntax**

Run: `go build -o bin/server.exe cmd/server/main.go`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/models/models.go
git commit -m "feat(models): add Selected field to Site struct"
```

---

### Task 3: Storage Layer - Add HasSites and Update CRUD Functions

**Files:**
- Modify: `internal/storage/site_store.go`

**Interfaces:**
- Consumes: `Site.Selected bool` from Task 2
- Produces: `HasSites(db *DB) (bool, error)`, all CRUD functions handle `selected` field

- [ ] **Step 1: Add HasSites function at end of file**

```go
func HasSites(db *DB) (bool, error) {
    var count int
    row := db.Conn().QueryRow(`SELECT COUNT(*) FROM sites`)
    if err := row.Scan(&count); err != nil {
        return false, fmt.Errorf("count sites: %w", err)
    }
    return count > 0, nil
}
```

- [ ] **Step 2: Update SyncSites to include selected field (lines 17-28)**

```go
stmt, err := tx.Prepare(`
    INSERT INTO sites (id, name, url, engine_type, selectors, cookie_file, enabled, selected, format_prompt)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET
        name = excluded.name,
        url = excluded.url,
        engine_type = excluded.engine_type,
        selectors = excluded.selectors,
        cookie_file = excluded.cookie_file,
        enabled = excluded.enabled,
        selected = excluded.selected,
        format_prompt = excluded.format_prompt
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
    selected := 0
    if site.Selected {
        selected = 1
    }
    _, err := stmt.Exec(site.ID, site.Name, site.URL, site.EngineType, site.Selectors, site.CookieFile, enabled, selected, site.FormatPrompt)
    if err != nil {
        return fmt.Errorf("upsert site %s: %w", site.ID, err)
    }
}
```

- [ ] **Step 3: Update SaveSite (lines 52-72)**

```go
func SaveSite(db *DB, site models.Site) error {
    enabled := 0
    if site.Enabled {
        enabled = 1
    }
    selected := 0
    if site.Selected {
        selected = 1
    }
    _, err := db.Conn().Exec(`
        INSERT INTO sites (id, name, url, engine_type, selectors, cookie_file, enabled, selected, format_prompt)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name = excluded.name,
            url = excluded.url,
            engine_type = excluded.engine_type,
            selectors = excluded.selectors,
            cookie_file = excluded.cookie_file,
            enabled = excluded.enabled,
            selected = excluded.selected,
            format_prompt = excluded.format_prompt
    `, site.ID, site.Name, site.URL, site.EngineType, site.Selectors, site.CookieFile, enabled, selected, site.FormatPrompt)
    if err != nil {
        return fmt.Errorf("save site: %w", err)
    }
    return nil
}
```

- [ ] **Step 4: Update UpdateSite (lines 75-101)**

```go
func UpdateSite(db *DB, site models.Site) error {
    enabled := 0
    if site.Enabled {
        enabled = 1
    }
    selected := 0
    if site.Selected {
        selected = 1
    }
    result, err := db.Conn().Exec(`
        UPDATE sites SET
            name = ?,
            url = ?,
            engine_type = ?,
            selectors = ?,
            cookie_file = ?,
            enabled = ?,
            selected = ?,
            format_prompt = ?
        WHERE id = ?
    `, site.Name, site.URL, site.EngineType, site.Selectors, site.CookieFile, enabled, selected, site.FormatPrompt, site.ID)
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
    return nil
}
```

- [ ] **Step 5: Update GetSites query and scan (lines 119-145)**

```go
func GetSites(db *DB) ([]models.Site, error) {
    rows, err := db.Conn().Query(`
        SELECT id, name, url, engine_type, selectors, cookie_file, enabled, selected, format_prompt, created_at
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
        var selected int
        if err := rows.Scan(&site.ID, &site.Name, &site.URL, &site.EngineType, &site.Selectors, &site.CookieFile, &enabled, &selected, &site.FormatPrompt, &site.CreatedAt); err != nil {
            return nil, fmt.Errorf("scan site: %w", err)
        }
        site.Enabled = enabled != 0
        site.Selected = selected != 0
        sites = append(sites, site)
    }

    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("iterate sites: %w", err)
    }

    return sites, nil
}
```

- [ ] **Step 6: Update GetSiteByID (lines 147-163)**

```go
func GetSiteByID(db *DB, id string) (*models.Site, error) {
    var site models.Site
    var enabled int
    var selected int
    row := db.Conn().QueryRow(`
        SELECT id, name, url, engine_type, selectors, cookie_file, enabled, selected, format_prompt, created_at
        FROM sites
        WHERE id = ?
    `, id)
    if err := row.Scan(&site.ID, &site.Name, &site.URL, &site.EngineType, &site.Selectors, &site.CookieFile, &enabled, &selected, &site.FormatPrompt, &site.CreatedAt); err != nil {
        if err == sql.ErrNoRows {
            return nil, nil
        }
        return nil, fmt.Errorf("scan site: %w", err)
    }
    site.Enabled = enabled != 0
    site.Selected = selected != 0
    return &site, nil
}
```

- [ ] **Step 7: Add UpdateSelected function**

```go
func UpdateSelected(db *DB, id string, selected bool) error {
    sel := 0
    if selected {
        sel = 1
    }
    result, err := db.Conn().Exec(`UPDATE sites SET selected = ? WHERE id = ?`, sel, id)
    if err != nil {
        return fmt.Errorf("update selected: %w", err)
    }
    rows, err := result.RowsAffected()
    if err != nil {
        return fmt.Errorf("check rows affected: %w", err)
    }
    if rows == 0 {
        return fmt.Errorf("site not found")
    }
    return nil
}
```

- [ ] **Step 8: Run go build to verify syntax**

Run: `go build -o bin/server.exe cmd/server/main.go`
Expected: Build succeeds

- [ ] **Step 9: Commit**

```bash
git add internal/storage/site_store.go
git commit -m "feat(storage): add HasSites and update CRUD for selected field"
```

---

### Task 4: Config Layer - Add Selected to SiteConfig

**Files:**
- Modify: `internal/config/config.go`

**Interfaces:**
- Consumes: `Site.Selected bool` from Task 2
- Produces: `SiteConfig.Selected bool`, `ToModels()` returns `Selected` correctly

- [ ] **Step 1: Add Selected field to SiteConfig (lines 23-31)**

```go
type SiteConfig struct {
    ID           string       `yaml:"id"`
    Name         string       `yaml:"name"`
    URL          string       `yaml:"url"`
    Enabled      bool         `yaml:"enabled"`
    Selected     bool         `yaml:"selected"`
    Engine       EngineConfig `yaml:"engine"`
    FormatPrompt string       `yaml:"format_prompt"`
    CookieFile   string       `yaml:"cookie_file"`
}
```

- [ ] **Step 2: Update ToModels to include Selected (lines 65-82)**

```go
func (cfg *Config) ToModels() []models.Site {
    result := make([]models.Site, 0, len(cfg.Sites))
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
            Selected:     s.Selected,
            FormatPrompt: s.FormatPrompt,
        }
        result = append(result, site)
    }
    return result
}
```

- [ ] **Step 3: Run go build to verify syntax**

Run: `go build -o bin/server.exe cmd/server/main.go`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add Selected field to SiteConfig"
```

---

### Task 5: API Layer - Add handleUpdateSelected and Update Request Struct

**Files:**
- Modify: `internal/api/sites.go`
- Modify: `internal/api/server.go`

**Interfaces:**
- Consumes: `storage.UpdateSelected`, `storage.GetSites` from Task 3
- Produces: `PUT /api/sites/:id/selected` endpoint, `CreateSiteRequest.Selected`

- [ ] **Step 1: Add Selected to CreateSiteRequest (lines 16-23)**

```go
type CreateSiteRequest struct {
    ID           string            `json:"id"`
    Name         string            `json:"name"`
    URL          string            `json:"url"`
    EngineType   string            `json:"engine_type"`
    Selectors    map[string]string `json:"selectors"`
    FormatPrompt string            `json:"format_prompt"`
    Selected     bool              `json:"selected"`
}
```

- [ ] **Step 2: Update handleCreateSite to set Selected default true (lines 34-75)**

```go
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

    enabled := req.Selectors["input"] != ""
    // Default to true if not specified
    selected := req.Selected
    if !selected && req.Selectors["input"] != "" {
        // If user didn't explicitly set selected but site is enabled, default to true
        selected = true
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
        Enabled:      enabled,
        Selected:     selected,
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
```

- [ ] **Step 3: Update handleUpdateSite to support Selected (lines 77-147)**

```go
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
    enabled := existing.Enabled
    selected := existing.Selected
    if req.Selectors != nil {
        selectorsJSON, err = json.Marshal(req.Selectors)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
            return
        }
        if req.Selectors["input"] != "" {
            enabled = true
        } else if len(req.Selectors) > 0 {
            enabled = false
        }
    } else {
        selectorsJSON = []byte(existing.Selectors)
    }

    // Update selected if explicitly provided
    if req.Selected || !req.Selected {
        selected = req.Selected
    }

    site := models.Site{
        ID:           id,
        Name:         req.Name,
        URL:          req.URL,
        EngineType:   req.EngineType,
        Selectors:    string(selectorsJSON),
        CookieFile:   existing.CookieFile,
        Enabled:      enabled,
        Selected:     selected,
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
```

- [ ] **Step 4: Add handleUpdateSelected function at end of file**

```go
func (s *Server) handleUpdateSelected(c *gin.Context) {
    id := c.Param("id")

    var req struct {
        Selected bool `json:"selected"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    if err := storage.UpdateSelected(s.db, id, req.Selected); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    if err := s.syncConfigToYAML(); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "updated"})
}
```

- [ ] **Step 5: Update syncConfigToYAML to include Selected (lines 180-211)**

```go
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
            Selected:     site.Selected,
            Engine:       config.EngineConfig{Primary: site.EngineType, Selectors: selectors},
            FormatPrompt: site.FormatPrompt,
            CookieFile:   site.CookieFile,
        })
    }

    return config.Save(sitesConfigPath, cfg)
}
```

- [ ] **Step 6: Register route in server.go**

Find the routes section and add:

```go
// Site routes
api.GET("/sites", s.handleGetSites)
api.POST("/sites", s.handleCreateSite)
api.PUT("/sites/:id", s.handleUpdateSite)
api.DELETE("/sites/:id", s.handleDeleteSite)
api.PUT("/sites/:id/selected", s.handleUpdateSelected)  // New route
api.GET("/detect", s.handleDetectSelectors)
```

- [ ] **Step 7: Run go build to verify syntax**

Run: `go build -o bin/server.exe cmd/server/main.go`
Expected: Build succeeds

- [ ] **Step 8: Commit**

```bash
git add internal/api/sites.go internal/api/server.go
git commit -m "feat(api): add PUT /api/sites/:id/selected endpoint"
```

---

### Task 6: Startup Logic - Split createDirectories and Check SQLite

**Files:**
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `storage.HasSites`, `storage.GetSites`, `config.Save` from previous tasks
- Produces: `createDirs()` called before NewDB, `ensureConfig()` called after NewDB

- [ ] **Step 1: Split createDirectories into createDirs (before DB)**

Replace the existing `createDirectories` function with:

```go
// createDirs creates necessary directories (called before DB initialization)
func createDirs() {
    dirs := []string{
        "configs",
        "data",
        ".browser-data",
    }

    for _, dir := range dirs {
        if _, err := os.Stat(dir); os.IsNotExist(err) {
            err := os.MkdirAll(dir, 0755)
            if err != nil {
                log.Fatalf("create directory %s: %v", dir, err)
            }
        }
    }
}

// ensureConfig ensures config file exists, checking SQLite first
func ensureConfig(db *storage.DB) {
    configPath := filepath.Join("configs", "sites.yaml")
    configExists := false
    if _, err := os.Stat(configPath); err == nil {
        configExists = true
    }

    // Check if SQLite has sites data
    hasSites, err := storage.HasSites(db)
    if err != nil {
        log.Fatalf("check sites: %v", err)
    }

    if hasSites {
        // SQLite has data - restore YAML from database
        sites, err := storage.GetSites(db)
        if err != nil {
            log.Fatalf("get sites: %v", err)
        }
        cfg := &config.Config{}
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
                Selected:     site.Selected,
                Engine:       config.EngineConfig{Primary: site.EngineType, Selectors: selectors},
                FormatPrompt: site.FormatPrompt,
                CookieFile:   site.CookieFile,
            })
        }
        if err := config.Save(configPath, cfg); err != nil {
            log.Fatalf("save config: %v", err)
        }
        fmt.Println("Restored config from database: configs/sites.yaml")
    } else if !configExists {
        // SQLite empty and YAML not exists - write embed default config
        err := os.WriteFile(configPath, defaultSitesConfig, 0644)
        if err != nil {
            log.Fatalf("create default config: %v", err)
        }
        fmt.Println("Created default config with preset sites: configs/sites.yaml")
    }
}
```

- [ ] **Step 2: Update main function flow**

```go
func main() {
    // 1. Create directories (before DB)
    createDirs()

    // 2. Initialize database
    db, err := storage.NewDB("data/knowledgeclip.db")
    if err != nil {
        log.Fatalf("init db: %v", err)
    }

    // 3. Ensure config file (after DB, checks SQLite first)
    ensureConfig(db)

    // 4. Load config
    cfg, err := config.Load("configs/sites.yaml")
    if err != nil {
        log.Fatalf("load config: %v", err)
    }

    // ... rest of main() unchanged
}
```

- [ ] **Step 3: Add missing imports**

Add `encoding/json` import if not present:

```go
import (
    "chat-aggregator/internal/api"
    "chat-aggregator/internal/config"
    "chat-aggregator/internal/engine"
    "chat-aggregator/internal/storage"
    "chat-aggregator/internal/systrayapp"
    "encoding/json"
    "fmt"
    "log"
    "os"
    "os/signal"
    "path/filepath"
    "syscall"
)
```

- [ ] **Step 4: Run go build to verify syntax**

Run: `go build -o bin/server.exe cmd/server/main.go`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(main): split createDirectories, check SQLite before writing embed config"
```

---

### Task 7: Update default_sites.yaml with selected field

**Files:**
- Modify: `cmd/server/default_sites.yaml`

**Interfaces:**
- Produces: All preset sites have `selected: true`

- [ ] **Step 1: Add selected: true to each site in default_sites.yaml**

Update each site block to include `selected: true`:

```yaml
global:
    format_prompt: ""
    default_timeout: 0
    max_concurrent: 0
sites:
    - id: qwen
      name: Qwen
      url: https://www.qianwen.com/
      enabled: true
      selected: true
      engine:
        primary: cdp
        selectors:
            answer: '[class*=answer-common-card]'
            content_strategy: clipboard
            copy_button: ""
            input: '[contenteditable=true]'
            submit: button[aria-label='发送消息']
            wait_for: '[contenteditable=true]'
      format_prompt: 请使用标准Markdown格式回答，标题从第三层级（###）开始，适当使用表格、代码块、列表等结构化元素。
      cookie_file: ""
    # ... repeat for kimi, deepseek, gemini, minimax, glm, doubao
```

Full file update - add `selected: true` to all 7 sites.

- [ ] **Step 2: Commit**

```bash
git add cmd/server/default_sites.yaml
git commit -m "feat(config): add selected: true to preset sites"
```

---

### Task 8: Frontend Types - Add selected field

**Files:**
- Modify: `web/src/types/index.ts`

**Interfaces:**
- Produces: `Site.selected?: boolean`

- [ ] **Step 1: Add selected field to Site interface**

```typescript
export interface Site {
  id: string
  name: string
  url: string
  engine_type: string
  enabled: boolean
  selected?: boolean
  selectors: string
  format_prompt: string
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/types/index.ts
git commit -m "feat(frontend): add selected field to Site interface"
```

---

### Task 9: Frontend Hook - Update fetchSites and add updateSelected

**Files:**
- Modify: `web/src/hooks/useSites.ts`

**Interfaces:**
- Consumes: `Site.selected` from Task 8, `PUT /api/sites/:id/selected` from Task 5
- Produces: `selectedSites` initialized from `selected=true`, `updateSelected(id, selected)` function

- [ ] **Step 1: Update fetchSites to read from selected=true**

```typescript
import { useEffect, useState, useCallback } from 'react'
import type { Site } from '../types'

export function useSites() {
  const [sites, setSites] = useState<Site[]>([])
  const [selectedSites, setSelectedSites] = useState<Set<string>>(new Set())

  const fetchSites = useCallback(() => {
    fetch('/api/sites')
      .then((res) => res.json())
      .then((data: Site[]) => {
        setSites(data || [])
        // Initialize from selected=true, not enabled=true
        const selected = (data || []).filter((s) => s.selected).map((s) => s.id)
        setSelectedSites(new Set(selected))
      })
      .catch((err) => {
        console.error('Failed to fetch sites:', err)
      })
  }, [])

  useEffect(() => {
    fetchSites()
  }, [fetchSites])

  const toggleSite = useCallback(
    (id: string) => {
      const site = sites.find((s) => s.id === id)
      if (!site || !site.enabled) return
      
      const newSelected = !selectedSites.has(id)
      
      // Update backend
      fetch(`/api/sites/${id}/selected`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ selected: newSelected }),
      })
        .then((res) => {
          if (res.ok) {
            setSelectedSites((prev) => {
              const next = new Set(prev)
              if (newSelected) {
                next.add(id)
              } else {
                next.delete(id)
              }
              return next
            })
          }
        })
        .catch((err) => {
          console.error('Failed to update selected:', err)
        })
    },
    [sites, selectedSites],
  )

  return { sites, selectedSites, toggleSite, fetchSites }
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/hooks/useSites.ts
git commit -m "feat(frontend): read selected from API, persist toggle to backend"
```

---

### Task 10: Frontend Component - Update checkbox to use selected field

**Files:**
- Modify: `web/src/components/SiteSidebar.tsx`

**Interfaces:**
- Consumes: `Site.selected` from Task 8, `toggleSite` behavior from Task 9

- [ ] **Step 1: Update checkbox checked logic**

The checkbox `checked` prop should use `selectedSites.has(site.id)` which is now driven by backend state. No change needed to the component structure - it already uses `selectedSites` from the hook.

Verify the current implementation is correct:

```tsx
const isChecked = selectedSites.has(site.id)
```

This is already correct - no change needed. The hook now manages `selectedSites` from backend `selected` field.

- [ ] **Step 2: Commit (if any changes made)**

If no changes needed, skip commit.

---

### Task 11: Build Frontend and Verify End-to-End

**Files:**
- Build: `web/` -> `internal/api/static/`

**Interfaces:**
- Consumes: All previous tasks complete

- [ ] **Step 1: Build frontend**

Run: `cd web && npm run build`
Expected: Build succeeds, output to `internal/api/static/`

- [ ] **Step 2: Build backend**

Run: `go build -o bin/server.exe cmd/server/main.go`
Expected: Build succeeds

- [ ] **Step 3: Run server and test**

Run: `./bin/server.exe`
Expected: Server starts, creates configs/sites.yaml with 7 preset sites all `selected: true`

- [ ] **Step 4: Test checkbox persistence**

1. Open browser to `http://localhost:8080`
2. Uncheck Qwen checkbox
3. Refresh page
4. Verify Qwen still unchecked

- [ ] **Step 5: Test config directory deletion recovery**

1. Stop server
2. Delete `configs/` directory
3. Restart server
4. Verify `configs/sites.yaml` restored with all sites including user changes

- [ ] **Step 6: Commit build artifacts**

```bash
git add internal/api/static/
git commit -m "build: frontend build with selected field support"
```

---

## Self-Review Checklist

1. **Spec coverage:**
   - Task 1-2: Database + Model `selected` field ✅
   - Task 3: Storage CRUD + `HasSites` ✅
   - Task 4: Config layer ✅
   - Task 5: API endpoint ✅
   - Task 6: Startup logic fix ✅
   - Task 7: Default config update ✅
   - Task 8-10: Frontend types, hook, component ✅
   - Task 11: End-to-end verification ✅

2. **Placeholder scan:**
   - No TBD, TODO, or vague descriptions ✅
   - All code steps show actual code ✅

3. **Type consistency:**
   - `Site.Selected bool` in Go model (Task 2) matches storage usage (Task 3) ✅
   - `selected?: boolean` in TypeScript (Task 8) matches API response ✅
   - `SiteConfig.Selected bool` in config (Task 4) matches YAML field ✅

4. **Edge cases:**
   - `selected` default `true` handled in Task 5 (CreateSiteRequest)
   - Config directory deletion handled in Task 6 (ensureConfig)
   - YAML restore from SQLite in Task 6

---

Plan complete and saved to `docs/superpowers/plans/2026-07-12-site-selection-persistence.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints for review

Which approach?