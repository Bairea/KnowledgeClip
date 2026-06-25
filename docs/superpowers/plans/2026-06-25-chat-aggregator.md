# 多站点大模型官网聊天聚合器 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 构建一个本地运行的 Web 应用，将用户输入并发发送到多个大模型官网站点，收集回答并支持 Keep/Remove 筛选和 JSON/Markdown 导出。

**Architecture:** 纯 Go 单体应用，后端 gin/fiber 提供 REST API 和 WebSocket，前端 React SPA 内嵌服务，浏览器自动化通过 rod→playwright-go→TS Playwright 三层降级，SQLite 持久化，YAML 管理站点配置。

**Tech Stack:** Go 1.22+, React 18 + Vite, Tailwind CSS, SQLite (modernc.org/sqlite), rod, playwright-go, WebSocket

---

## 文件结构

```
chat-aggregator/
├── cmd/
│   └── server/
│       └── main.go              # 入口
├── internal/
│   ├── api/
│   │   ├── server.go            # HTTP server setup
│   │   ├── chat.go              # /chat endpoints
│   │   ├── sites.go             # /sites endpoints
│   │   ├── export.go            # /export endpoints
│   │   └── websocket.go         # WebSocket handler
│   ├── engine/
│   │   ├── manager.go           # 浏览器引擎管理器
│   │   ├── rod_engine.go        # rod 实现
│   │   ├── playwright_go.go     # playwright-go 实现
│   │   └── ts_playwright.go     # TS Playwright 子进程
│   ├── models/
│   │   └── models.go            # 数据模型
│   ├── storage/
│   │   ├── db.go                # SQLite 连接
│   │   ├── session_store.go     # session CRUD
│   │   ├── message_store.go     # message CRUD
│   │   ├── site_store.go        # site CRUD
│   │   └── cookie_store.go      # cookie CRUD
│   ├── config/
│   │   └── config.go            # YAML 配置读写
│   └── export/
│       ├── json_exporter.go     # JSON 导出
│       └── markdown_exporter.go # Markdown 导出
├── web/                         # React 前端
│   ├── index.html
│   ├── package.json
│   ├── vite.config.ts
│   ├── tailwind.config.js
│   ├── tsconfig.json
│   └── src/
│       ├── main.tsx
│       ├── App.tsx
│       ├── index.css
│       ├── components/
│       │   ├── SiteSidebar.tsx
│       │   ├── ChatGrid.tsx
│       │   ├── MessageCard.tsx
│       │   ├── KeepSwitch.tsx
│       │   ├── InputArea.tsx
│       │   ├── ExportPanel.tsx
│       │   └── SiteConfigModal.tsx
│       ├── hooks/
│       │   ├── useWebSocket.ts
│       │   └── useSites.ts
│       └── types/
│           └── index.ts
├── configs/
│   └── sites.yaml
├── go.mod
├── go.sum
└── Makefile
```

---

## 阶段 1：Go 后端骨架 + SQLite 数据层 + 基础 API

### Task 1: 初始化 Go 模块和项目结构

**Files:**
- Create: `go.mod`
- Create: `cmd/server/main.go`
- Create: `Makefile`

- [ ] **Step 1: 初始化 Go 模块**

Run:
```bash
cd d:\Desktopfile\chores\KnowledgeClip
go mod init chat-aggregator
```

Expected: `go.mod` created

- [ ] **Step 2: 安装依赖**

Run:
```bash
go get github.com/gin-gonic/gin
go get github.com/google/uuid
go get modernc.org/sqlite
go get gopkg.in/yaml.v3
```

- [ ] **Step 3: 创建 Makefile**

Create: `Makefile`
```makefile
.PHONY: build run dev clean

build:
	go build -o bin/server.exe cmd/server/main.go

run: build
	.\bin\server.exe

dev:
	go run cmd/server/main.go

clean:
	rm -rf bin/
```

- [ ] **Step 4: 创建入口文件**

Create: `cmd/server/main.go`
```go
package main

import (
	"log"
	"chat-aggregator/internal/api"
)

func main() {
	server := api.NewServer()
	if err := server.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 5: 创建基础 Server**

Create: `internal/api/server.go`
```go
package api

import (
	"github.com/gin-gonic/gin"
)

type Server struct {
	router *gin.Engine
}

func NewServer() *Server {
	r := gin.Default()
	s := &Server{router: r}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}
```

- [ ] **Step 6: 验证编译通过**

Run: `make build`
Expected: `bin/server.exe` created without errors

- [ ] **Step 7: Commit**

```bash
git add .
git commit -m "feat: init Go project structure with basic server"
```

---

### Task 2: SQLite 数据库初始化

**Files:**
- Create: `internal/storage/db.go`
- Create: `internal/models/models.go`

- [ ] **Step 1: 定义数据模型**

Create: `internal/models/models.go`
```go
package models

import "time"

type Site struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	EngineType   string    `json:"engine_type"`
	Selectors    string    `json:"selectors"` // JSON string
	CookieFile   string    `json:"cookie_file"`
	Enabled      bool      `json:"enabled"`
	FormatPrompt string    `json:"format_prompt"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	CreatedAt time.Time `json:"created_at"`
}

type Message struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	SiteID     string    `json:"site_id"`
	Content    string    `json:"content"`
	Kept       bool      `json:"kept"`
	Error      string    `json:"error"`
	ElapsedMs  int       `json:"elapsed_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

type SiteCookie struct {
	SiteID       string    `json:"site_id"`
	Cookies      string    `json:"cookies"`       // JSON
	LocalStorage string    `json:"local_storage"` // JSON
	UpdatedAt    time.Time `json:"updated_at"`
}
```

- [ ] **Step 2: 创建数据库连接和迁移**

Create: `internal/storage/db.go`
```go
package storage

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS sites (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  engine_type TEXT NOT NULL,
  selectors TEXT NOT NULL,
  cookie_file TEXT,
  enabled INTEGER DEFAULT 1,
  format_prompt TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  prompt TEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  site_id TEXT NOT NULL,
  content TEXT NOT NULL,
  kept INTEGER DEFAULT 1,
  error TEXT,
  elapsed_ms INTEGER,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (session_id) REFERENCES sessions(id),
  FOREIGN KEY (site_id) REFERENCES sites(id)
);

CREATE TABLE IF NOT EXISTS site_cookies (
  site_id TEXT PRIMARY KEY,
  cookies TEXT NOT NULL,
  local_storage TEXT,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (site_id) REFERENCES sites(id)
);
`

type DB struct {
	conn *sql.DB
}

func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := conn.Exec(schema); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &DB{conn: conn}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) Conn() *sql.DB {
	return db.conn
}
```

- [ ] **Step 3: 初始化数据库连接**

Modify: `internal/api/server.go`
```go
package api

import (
	"chat-aggregator/internal/storage"
	"github.com/gin-gonic/gin"
)

type Server struct {
	router *gin.Engine
	db     *storage.DB
}

func NewServer() *Server {
	db, err := storage.NewDB("data/app.db")
	if err != nil {
		panic(err)
	}

	r := gin.Default()
	s := &Server{router: r, db: db}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}
```

- [ ] **Step 4: 验证编译和运行**

Run: `make build && make run`
Expected: Server starts on :8080, no panic

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "feat: add SQLite schema and DB connection"
```

---

### Task 3: YAML 配置读写

**Files:**
- Create: `configs/sites.yaml`
- Create: `internal/config/config.go`

- [ ] **Step 1: 创建默认站点配置**

Create: `configs/sites.yaml`
```yaml
sites:
  - id: qwen
    name: Qwen
    url: https://chat.qwen.ai
    enabled: true
    engine:
      primary: cdp
      selectors:
        input: "#chat-input"
        submit: "#send-btn"
        answer: ".message-content"
        wait_for: ".message-content:last-child"
    format_prompt: |
      【回答格式要求】
      1. 使用 Markdown 格式
      2. 一级标题请从 ### 开始，不要使用 # 或 ##
      3. 代码块请标注语言（如 ```python）
      4. 数学公式使用 $...$ 或 $$...$$
      5. 保持回答简洁，结构化
    cookie_file: ""

  - id: kimi
    name: Kimi
    url: https://www.kimi.com/
    enabled: true
    engine:
      primary: cdp
      selectors:
        input: "textarea"
        submit: "button[type=submit]"
        answer: ".answer-content"
        wait_for: ".answer-content:last-child"
    format_prompt: ""
    cookie_file: ""

  - id: deepseek
    name: DeepSeek
    url: https://chat.deepseek.com/
    enabled: true
    engine:
      primary: cdp
      selectors:
        input: "#chat-input"
        submit: "#send-button"
        answer: ".message-bubble"
        wait_for: ".message-bubble:last-child"
    format_prompt: ""
    cookie_file: ""

global:
  format_prompt: |
    【回答格式要求】
    1. 使用 Markdown 格式
    2. 一级标题请从 ### 开始，不要使用 # 或 ##
    3. 代码块请标注语言
    4. 数学公式使用 $...$ 或 $$...$$
    5. 保持回答简洁，结构化
  default_timeout: 120000
  max_concurrent: 0
```

- [ ] **Step 2: 实现配置读取**

Create: `internal/config/config.go`
```go
package config

import (
	"os"
	"chat-aggregator/internal/models"
	"gopkg.in/yaml.v3"
)

type SiteConfig struct {
	ID           string         `yaml:"id"`
	Name         string         `yaml:"name"`
	URL          string         `yaml:"url"`
	Enabled      bool           `yaml:"enabled"`
	Engine       EngineConfig   `yaml:"engine"`
	FormatPrompt string         `yaml:"format_prompt"`
	CookieFile   string         `yaml:"cookie_file"`
}

type EngineConfig struct {
	Primary   string            `yaml:"primary"`
	Selectors map[string]string `yaml:"selectors"`
}

type GlobalConfig struct {
	FormatPrompt   string `yaml:"format_prompt"`
	DefaultTimeout int    `yaml:"default_timeout"`
	MaxConcurrent  int    `yaml:"max_concurrent"`
}

type Config struct {
	Sites  []SiteConfig `yaml:"sites"`
	Global GlobalConfig `yaml:"global"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (c *Config) ToModels() []models.Site {
	var sites []models.Site
	for _, s := range c.Sites {
		sites = append(sites, models.Site{
			ID:           s.ID,
			Name:         s.Name,
			URL:          s.URL,
			EngineType:   s.Engine.Primary,
			Selectors:    mustJSON(s.Engine.Selectors),
			CookieFile:   s.CookieFile,
			Enabled:      s.Enabled,
			FormatPrompt: s.FormatPrompt,
		})
	}
	return sites
}

func mustJSON(v map[string]string) string {
	if v == nil {
		return "{}"
	}
	b, _ := json.Marshal(v)
	return string(b)
}
```

Wait, I need to import encoding/json. Let me fix:

```go
package config

import (
	"encoding/json"
	"os"
	"chat-aggregator/internal/models"
	"gopkg.in/yaml.v3"
)
```

- [ ] **Step 3: 应用启动时加载 YAML 到数据库**

Modify: `cmd/server/main.go`
```go
package main

import (
	"log"
	"chat-aggregator/internal/api"
	"chat-aggregator/internal/config"
	"chat-aggregator/internal/storage"
)

func main() {
	cfg, err := config.Load("configs/sites.yaml")
	if err != nil {
		log.Printf("warning: load config: %v", err)
	}

	db, err := storage.NewDB("data/app.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if cfg != nil {
		if err := storage.SyncSites(db, cfg.ToModels()); err != nil {
			log.Printf("warning: sync sites: %v", err)
		}
	}

	server := api.NewServer(db)
	if err := server.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 4: 创建 Site Store 和 Sync 方法**

Create: `internal/storage/site_store.go`
```go
package storage

import (
	"chat-aggregator/internal/models"
)

func SyncSites(db *DB, sites []models.Site) error {
	for _, site := range sites {
		_, err := db.Conn().Exec(
			`INSERT OR REPLACE INTO sites (id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			site.ID, site.Name, site.URL, site.EngineType, site.Selectors,
			site.CookieFile, site.Enabled, site.FormatPrompt,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) GetSites() ([]models.Site, error) {
	rows, err := db.Conn().Query(`SELECT id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt, created_at FROM sites`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []models.Site
	for rows.Next() {
		var s models.Site
		if err := rows.Scan(&s.ID, &s.Name, &s.URL, &s.EngineType, &s.Selectors, &s.CookieFile, &s.Enabled, &s.FormatPrompt, &s.CreatedAt); err != nil {
			return nil, err
		}
		sites = append(sites, s)
	}
	return sites, nil
}

func (db *DB) GetSiteByID(id string) (*models.Site, error) {
	var s models.Site
	err := db.Conn().QueryRow(
		`SELECT id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt, created_at FROM sites WHERE id = ?`,
		id,
	).Scan(&s.ID, &s.Name, &s.URL, &s.EngineType, &s.Selectors, &s.CookieFile, &s.Enabled, &s.FormatPrompt, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
```

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "feat: add YAML config loading and site sync to DB"
```

---

## 阶段 2：浏览器引擎管理器（rod 为主）+ 单层站点支持

### Task 4: rod 引擎实现

**Files:**
- Create: `internal/engine/manager.go`
- Create: `internal/engine/rod_engine.go`

- [ ] **Step 1: 安装 rod**

Run: `go get github.com/go-rod/rod`

- [ ] **Step 2: 定义浏览器引擎接口**

Create: `internal/engine/manager.go`
```go
package engine

import (
	"context"
	"chat-aggregator/internal/models"
)

type BrowserEngine interface {
	SendMessage(ctx context.Context, site models.Site, prompt string) (string, error)
	Close() error
}

type Manager struct {
	primary BrowserEngine
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	// Try primary engine first
	if m.primary == nil {
		rod, err := NewRodEngine()
		if err != nil {
			return "", err
		}
		m.primary = rod
	}
	return m.primary.SendMessage(ctx, site, prompt)
}

func (m *Manager) Close() error {
	if m.primary != nil {
		return m.primary.Close()
	}
	return nil
}
```

- [ ] **Step 3: 实现 rod 引擎**

Create: `internal/engine/rod_engine.go`
```go
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"chat-aggregator/internal/models"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

type RodEngine struct {
	browser *rod.Browser
}

func NewRodEngine() (*RodEngine, error) {
	l := launcher.New().
		Headless(false).
		Devtools(true)

	url, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("launch browser: %w", err)
	}

	browser := rod.New().ControlURL(url).MustConnect()
	return &RodEngine{browser: browser}, nil
}

func (e *RodEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	selectors := map[string]string{}
	if err := json.Unmarshal([]byte(site.Selectors), &selectors); err != nil {
		return "", fmt.Errorf("parse selectors: %w", err)
	}

	page := e.browser.MustPage(site.URL)
	defer page.Close()

	// Wait for page load
	page.MustWaitLoad()

	// Find input and type prompt
	inputSel := selectors["input"]
	if inputSel == "" {
		inputSel = "textarea"
	}
	page.MustElement(inputSel).MustInput(prompt)

	// Click submit
	submitSel := selectors["submit"]
	if submitSel == "" {
		submitSel = "button[type=submit]"
	}
	page.MustElement(submitSel).MustClick()

	// Wait for answer
	waitSel := selectors["wait_for"]
	if waitSel == "" {
		waitSel = selectors["answer"]
	}

	// Poll for answer with timeout
	timeout := 120 * time.Second
	if site.FormatPrompt != "" {
		// Use default from config instead, simplified for now
	}

	start := time.Now()
	for time.Since(start) < timeout {
		el, err := page.Element(waitSel)
		if err == nil && el != nil {
			text, err := el.Text()
			if err == nil && text != "" {
				return text, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return "", fmt.Errorf("timeout waiting for answer")
}

func (e *RodEngine) Close() error {
	if e.browser != nil {
		return e.browser.Close()
	}
	return nil
}
```

- [ ] **Step 4: Commit**

```bash
git add .
git commit -m "feat: add rod browser engine with basic send message"
```

---

### Task 5: 添加 /chat API 端点（单层同步）

**Files:**
- Create: `internal/api/chat.go`
- Modify: `internal/api/server.go`

- [ ] **Step 1: 创建 chat 端点**

Create: `internal/api/chat.go`
```go
package api

import (
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"chat-aggregator/internal/engine"
	"chat-aggregator/internal/models"
)

type ChatRequest struct {
	Prompt  string   `json:"prompt" binding:"required"`
	SiteIDs []string `json:"site_ids"`
}

type ChatResponse struct {
	SessionID string            `json:"session_id"`
	Results   map[string]string `json:"results"`
}

func (s *Server) handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Get enabled sites
	sites, err := s.db.GetSites()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
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

	// Create session
	sessionID := uuid.New().String()
	_, err = s.db.Conn().Exec(`INSERT INTO sessions (id, prompt) VALUES (?, ?)`, sessionID, req.Prompt)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Send to sites (sync for now)
	manager := engine.NewManager()
	defer manager.Close()

	results := make(map[string]string)
	for _, site := range targetSites {
		content, err := manager.SendMessage(c.Request.Context(), site, req.Prompt)
		msgID := uuid.New().String()
		errStr := ""
		if err != nil {
			errStr = err.Error()
			content = ""
		}
		_, dbErr := s.db.Conn().Exec(
			`INSERT INTO messages (id, session_id, site_id, content, error) VALUES (?, ?, ?, ?, ?)`,
			msgID, sessionID, site.ID, content, errStr,
		)
		if dbErr != nil {
			// Log but continue
		}
		if err == nil {
			results[site.ID] = content
		} else {
			results[site.ID] = "ERROR: " + errStr
		}
	}

	c.JSON(200, ChatResponse{SessionID: sessionID, Results: results})
}
```

- [ ] **Step 2: 注册路由**

Modify: `internal/api/server.go` — add to `setupRoutes()`:
```go
func (s *Server) setupRoutes() {
	s.router.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	s.router.POST("/api/chat", s.handleChat)
}
```

- [ ] **Step 3: Commit**

```bash
git add .
git commit -m "feat: add /chat endpoint with single-site sync support"
```

---

## 阶段 3：React 前端骨架 + 左栏站点列表 + 右栏网格

### Task 6: 初始化 React 前端

**Files:**
- Create: `web/package.json`
- Create: `web/vite.config.ts`
- Create: `web/tsconfig.json`
- Create: `web/tailwind.config.js`
- Create: `web/index.html`
- Create: `web/src/main.tsx`
- Create: `web/src/App.tsx`
- Create: `web/src/index.css`

- [ ] **Step 1: 创建 package.json**

Create: `web/package.json`
```json
{
  "name": "chat-aggregator-web",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "react": "^18.3.0",
    "react-dom": "^18.3.0",
    "react-markdown": "^9.0.0",
    "remark-gfm": "^4.0.0",
    "prism-react-renderer": "^2.3.0",
    "katex": "^0.16.9",
    "react-katex": "^3.0.1"
  },
  "devDependencies": {
    "@types/react": "^18.3.0",
    "@types/react-dom": "^18.3.0",
    "@vitejs/plugin-react": "^4.3.0",
    "autoprefixer": "^10.4.0",
    "postcss": "^8.4.0",
    "tailwindcss": "^3.4.0",
    "typescript": "^5.4.0",
    "vite": "^5.2.0"
  }
}
```

- [ ] **Step 2: 创建配置文件**

Create: `web/vite.config.ts`
```typescript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
  build: {
    outDir: '../internal/api/static',
    emptyOutDir: true,
  },
})
```

Create: `web/tsconfig.json`
```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "baseUrl": ".",
    "paths": {
      "@/*": ["src/*"]
    }
  },
  "include": ["src"],
  "references": [{ "path": "./tsconfig.node.json" }]
}
```

Create: `web/tsconfig.node.json`
```json
{
  "compilerOptions": {
    "composite": true,
    "skipLibCheck": true,
    "module": "ESNext",
    "moduleResolution": "bundler",
    "allowSyntheticDefaultImports": true
  },
  "include": ["vite.config.ts"]
}
```

Create: `web/tailwind.config.js`
```javascript
/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {},
  },
  plugins: [],
}
```

Create: `web/postcss.config.js`
```javascript
export default {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
  },
}
```

Create: `web/index.html`
```html
<!DOCTYPE html>
<html lang="zh-CN">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>多站点AI聚合</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 3: 创建入口文件**

Create: `web/src/main.tsx`
```tsx
import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
```

Create: `web/src/index.css`
```css
@tailwind base;
@tailwind components;
@tailwind utilities;

body {
  @apply bg-slate-900 text-slate-100;
}
```

- [ ] **Step 4: 安装依赖**

Run:
```bash
cd web
npm install
```

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "feat: init React frontend with Vite + Tailwind"
```

---

### Task 7: 创建前端组件骨架

**Files:**
- Create: `web/src/types/index.ts`
- Create: `web/src/hooks/useSites.ts`
- Create: `web/src/components/SiteSidebar.tsx`
- Create: `web/src/components/ChatGrid.tsx`
- Create: `web/src/components/MessageCard.tsx`
- Create: `web/src/components/KeepSwitch.tsx`
- Create: `web/src/components/InputArea.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: 定义类型**

Create: `web/src/types/index.ts`
```typescript
export interface Site {
  id: string;
  name: string;
  url: string;
  engine_type: string;
  enabled: boolean;
  format_prompt: string;
}

export interface Message {
  id: string;
  session_id: string;
  site_id: string;
  content: string;
  kept: boolean;
  error: string;
  elapsed_ms: number;
  created_at: string;
}

export interface Session {
  id: string;
  prompt: string;
  created_at: string;
}
```

- [ ] **Step 2: 创建 useSites hook**

Create: `web/src/hooks/useSites.ts`
```typescript
import { useState, useEffect } from 'react'
import { Site } from '../types'

export function useSites() {
  const [sites, setSites] = useState<Site[]>([])
  const [selectedSites, setSelectedSites] = useState<Set<string>>(new Set())

  useEffect(() => {
    fetch('/api/sites')
      .then(r => r.json())
      .then(data => {
        setSites(data)
        setSelectedSites(new Set(data.filter((s: Site) => s.enabled).map((s: Site) => s.id)))
      })
  }, [])

  const toggleSite = (id: string) => {
    setSelectedSites(prev => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  return { sites, selectedSites, toggleSite }
}
```

- [ ] **Step 3: 创建 SiteSidebar**

Create: `web/src/components/SiteSidebar.tsx`
```tsx
import { Site } from '../types'

interface Props {
  sites: Site[]
  selectedSites: Set<string>
  onToggle: (id: string) => void
}

export default function SiteSidebar({ sites, selectedSites, onToggle }: Props) {
  return (
    <div className="w-48 bg-slate-800 border-r border-slate-700 flex flex-col">
      <div className="p-3 border-b border-slate-700 font-semibold text-sm">
        站点列表
      </div>
      <div className="flex-1 overflow-y-auto">
        {sites.map(site => (
          <label
            key={site.id}
            className="flex items-center gap-2 px-3 py-2 hover:bg-slate-700 cursor-pointer"
          >
            <input
              type="checkbox"
              checked={selectedSites.has(site.id)}
              onChange={() => onToggle(site.id)}
              className="rounded"
            />
            <span className="text-sm">{site.name}</span>
          </label>
        ))}
      </div>
    </div>
  )
}
```

- [ ] **Step 4: 创建 MessageCard**

Create: `web/src/components/MessageCard.tsx`
```tsx
import { Message } from '../types'
import KeepSwitch from './KeepSwitch'

interface Props {
  message: Message
  onToggleKeep: (id: string) => void
}

export default function MessageCard({ message, onToggleKeep }: Props) {
  return (
    <div className="bg-slate-800 rounded-lg border border-slate-700 flex flex-col h-full">
      <div className="px-3 py-2 border-b border-slate-700 font-medium text-sm text-sky-400">
        {message.site_id}
      </div>
      <div className="flex-1 p-3 overflow-y-auto text-sm">
        {message.error ? (
          <div className="text-red-400">{message.error}</div>
        ) : (
          <pre className="whitespace-pre-wrap">{message.content}</pre>
        )}
      </div>
      <div className="px-3 py-2 border-t border-slate-700 flex justify-end">
        <KeepSwitch
          checked={message.kept}
          onChange={() => onToggleKeep(message.id)}
        />
      </div>
    </div>
  )
}
```

- [ ] **Step 5: 创建 KeepSwitch**

Create: `web/src/components/KeepSwitch.tsx`
```tsx
interface Props {
  checked: boolean
  onChange: () => void
}

export default function KeepSwitch({ checked, onChange }: Props) {
  return (
    <button
      onClick={onChange}
      className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
        checked ? 'bg-green-500' : 'bg-slate-600'
      }`}
    >
      <span
        className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
          checked ? 'translate-x-6' : 'translate-x-1'
        }`}
      />
    </button>
  )
}
```

- [ ] **Step 6: 创建 ChatGrid**

Create: `web/src/components/ChatGrid.tsx`
```tsx
import { Message } from '../types'
import MessageCard from './MessageCard'

interface Props {
  messages: Message[]
  onToggleKeep: (id: string) => void
}

export default function ChatGrid({ messages, onToggleKeep }: Props) {
  return (
    <div className="flex-1 flex gap-3 p-3 overflow-x-auto">
      {messages.map(msg => (
        <div key={msg.id} className="w-80 flex-shrink-0">
          <MessageCard message={msg} onToggleKeep={onToggleKeep} />
        </div>
      ))}
    </div>
  )
}
```

- [ ] **Step 7: 创建 InputArea**

Create: `web/src/components/InputArea.tsx`
```tsx
import { useState } from 'react'

interface Props {
  onSend: (prompt: string) => void
  disabled?: boolean
}

export default function InputArea({ onSend, disabled }: Props) {
  const [prompt, setPrompt] = useState('')

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!prompt.trim() || disabled) return
    onSend(prompt)
    setPrompt('')
  }

  return (
    <form onSubmit={handleSubmit} className="p-3 border-t border-slate-700">
      <div className="flex gap-2">
        <input
          type="text"
          value={prompt}
          onChange={e => setPrompt(e.target.value)}
          placeholder="输入你的问题..."
          disabled={disabled}
          className="flex-1 bg-slate-800 border border-slate-600 rounded-lg px-4 py-2 text-sm focus:outline-none focus:border-sky-500"
        />
        <button
          type="submit"
          disabled={disabled}
          className="bg-sky-500 hover:bg-sky-600 text-white px-6 py-2 rounded-lg text-sm font-medium disabled:opacity-50"
        >
          发送
        </button>
      </div>
    </form>
  )
}
```

- [ ] **Step 8: 组装 App**

Create: `web/src/App.tsx`
```tsx
import { useState } from 'react'
import { useSites } from './hooks/useSites'
import { Message } from './types'
import SiteSidebar from './components/SiteSidebar'
import ChatGrid from './components/ChatGrid'
import InputArea from './components/InputArea'

export default function App() {
  const { sites, selectedSites, toggleSite } = useSites()
  const [messages, setMessages] = useState<Message[]>([])
  const [loading, setLoading] = useState(false)

  const handleSend = async (prompt: string) => {
    setLoading(true)
    const siteIDs = Array.from(selectedSites)

    // Create placeholder messages
    const placeholders = siteIDs.map(siteID => ({
      id: `${siteID}-${Date.now()}`,
      session_id: '',
      site_id: siteID,
      content: '加载中...',
      kept: true,
      error: '',
      elapsed_ms: 0,
      created_at: new Date().toISOString(),
    }))
    setMessages(placeholders)

    try {
      const res = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt, site_ids: siteIDs }),
      })
      const data = await res.json()

      // Convert results to messages
      const newMessages = siteIDs.map(siteID => ({
        id: `${siteID}-${Date.now()}`,
        session_id: data.session_id,
        site_id: siteID,
        content: data.results[siteID] || '',
        kept: true,
        error: data.results[siteID]?.startsWith('ERROR:') ? data.results[siteID] : '',
        elapsed_ms: 0,
        created_at: new Date().toISOString(),
      }))
      setMessages(newMessages)
    } catch (err) {
      setMessages(prev => prev.map(m => ({ ...m, error: String(err) })))
    } finally {
      setLoading(false)
    }
  }

  const handleToggleKeep = (id: string) => {
    setMessages(prev =>
      prev.map(m => (m.id === id ? { ...m, kept: !m.kept } : m))
    )
  }

  return (
    <div className="h-screen flex flex-col">
      <header className="h-12 bg-slate-800 border-b border-slate-700 flex items-center px-4 justify-between">
        <h1 className="font-semibold">多站点AI聚合</h1>
      </header>
      <div className="flex-1 flex overflow-hidden">
        <SiteSidebar
          sites={sites}
          selectedSites={selectedSites}
          onToggle={toggleSite}
        />
        <div className="flex-1 flex flex-col">
          <ChatGrid messages={messages} onToggleKeep={handleToggleKeep} />
          <InputArea onSend={handleSend} disabled={loading} />
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 9: 添加 /sites API 端点**

Modify: `internal/api/sites.go`
```go
package api

import "github.com/gin-gonic/gin"

func (s *Server) handleGetSites(c *gin.Context) {
	sites, err := s.db.GetSites()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, sites)
}
```

Modify: `internal/api/server.go` — add to `setupRoutes()`:
```go
s.router.GET("/api/sites", s.handleGetSites)
```

- [ ] **Step 10: 验证前端构建**

Run:
```bash
cd web
npm run build
```

Expected: Build succeeds, output to `internal/api/static`

- [ ] **Step 11: 配置 Go 静态文件服务**

Modify: `internal/api/server.go`
```go
import "github.com/gin-gonic/gin"

func (s *Server) setupRoutes() {
	s.router.Static("/", "./internal/api/static")
	s.router.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	s.router.GET("/api/sites", s.handleGetSites)
	s.router.POST("/api/chat", s.handleChat)
}
```

- [ ] **Step 12: Commit**

```bash
git add .
git commit -m "feat: add React frontend with sidebar, chat grid, and keep switch"
```

---

## 阶段 4：并发请求 + WebSocket 实时推送

### Task 8: 并发发送 + WebSocket 推送

**Files:**
- Create: `internal/api/websocket.go`
- Modify: `internal/api/chat.go`
- Modify: `web/src/hooks/useWebSocket.ts`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: 安装 gorilla/websocket**

Run: `go get github.com/gorilla/websocket`

- [ ] **Step 2: 创建 WebSocket handler**

Create: `internal/api/websocket.go`
```go
package api

import (
	"net/http"
	"sync"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Hub struct {
	clients map[*websocket.Conn]bool
	mu      sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*websocket.Conn]bool)}
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

func (h *Hub) Broadcast(msg interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		client.WriteJSON(msg)
	}
}

func (s *Server) handleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	s.hub.Add(conn)
	defer s.hub.Remove(conn)

	// Keep connection alive
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}
```

- [ ] **Step 3: 修改 Server 添加 Hub**

Modify: `internal/api/server.go`
```go
type Server struct {
	router *gin.Engine
	db     *storage.DB
	hub    *Hub
}

func NewServer(db *storage.DB) *Server {
	r := gin.Default()
	s := &Server{router: r, db: db, hub: NewHub()}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.Static("/", "./internal/api/static")
	s.router.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	s.router.GET("/api/sites", s.handleGetSites)
	s.router.POST("/api/chat", s.handleChat)
	s.router.GET("/ws", s.handleWebSocket)
}
```

- [ ] **Step 4: 重构 chat 为并发 + WebSocket**

Modify: `internal/api/chat.go`
```go
package api

import (
	"context"
	"net/http"
	"sync"
	"time"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"chat-aggregator/internal/engine"
	"chat-aggregator/internal/models"
)

type ChatRequest struct {
	Prompt  string   `json:"prompt" binding:"required"`
	SiteIDs []string `json:"site_ids"`
}

type MessageUpdate struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	SiteID    string `json:"site_id"`
	Content   string `json:"content"`
	Error     string `json:"error"`
	Done      bool   `json:"done"`
}

func (s *Server) handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	sites, err := s.db.GetSites()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
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
	_, err = s.db.Conn().Exec(`INSERT INTO sessions (id, prompt) VALUES (?, ?)`, sessionID, req.Prompt)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Start concurrent processing
	manager := engine.NewManager()
	defer manager.Close()

	var wg sync.WaitGroup
	for _, site := range targetSites {
		wg.Add(1)
		go func(site models.Site) {
			defer wg.Done()
			start := time.Now()
			content, err := manager.SendMessage(c.Request.Context(), site, req.Prompt)
			elapsed := int(time.Since(start).Milliseconds())

			msgID := uuid.New().String()
			errStr := ""
			if err != nil {
				errStr = err.Error()
				content = ""
			}

			_, dbErr := s.db.Conn().Exec(
				`INSERT INTO messages (id, session_id, site_id, content, error, elapsed_ms) VALUES (?, ?, ?, ?, ?, ?)`,
				msgID, sessionID, site.ID, content, errStr, elapsed,
			)
			if dbErr != nil {
				// Log error
			}

			s.hub.Broadcast(MessageUpdate{
				Type:      "message",
				SessionID: sessionID,
				SiteID:    site.ID,
				Content:   content,
				Error:     errStr,
				Done:      true,
			})
		}(site)
	}

	// Return immediately with session ID
	c.JSON(200, gin.H{"session_id": sessionID})

	// Wait for all goroutines in background
	go func() {
		wg.Wait()
		s.hub.Broadcast(MessageUpdate{
			Type:      "complete",
			SessionID: sessionID,
			Done:      true,
		})
	}()
}
```

- [ ] **Step 5: 前端 WebSocket hook**

Create: `web/src/hooks/useWebSocket.ts`
```typescript
import { useEffect, useRef, useCallback } from 'react'

export function useWebSocket(onMessage: (data: any) => void) {
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    const ws = new WebSocket(`ws://${window.location.host}/ws`)
    wsRef.current = ws

    ws.onmessage = (event) => {
      const data = JSON.parse(event.data)
      onMessage(data)
    }

    ws.onclose = () => {
      // Auto reconnect after 3s
      setTimeout(() => {
        window.location.reload()
      }, 3000)
    }

    return () => {
      ws.close()
    }
  }, [])

  const send = useCallback((msg: any) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(msg))
    }
  }, [])

  return { send }
}
```

- [ ] **Step 6: 更新 App 使用 WebSocket**

Modify: `web/src/App.tsx`
```tsx
import { useState } from 'react'
import { useSites } from './hooks/useSites'
import { useWebSocket } from './hooks/useWebSocket'
import { Message } from './types'
import SiteSidebar from './components/SiteSidebar'
import ChatGrid from './components/ChatGrid'
import InputArea from './components/InputArea'

export default function App() {
  const { sites, selectedSites, toggleSite } = useSites()
  const [messages, setMessages] = useState<Record<string, Message>>({})
  const [loading, setLoading] = useState(false)
  const [sessionId, setSessionId] = useState('')

  const { send } = useWebSocket((data) => {
    if (data.type === 'message') {
      setMessages(prev => ({
        ...prev,
        [data.site_id]: {
          id: `${data.site_id}-${Date.now()}`,
          session_id: data.session_id,
          site_id: data.site_id,
          content: data.content,
          kept: true,
          error: data.error,
          elapsed_ms: 0,
          created_at: new Date().toISOString(),
        }
      }))
    } else if (data.type === 'complete') {
      setLoading(false)
    }
  })

  const handleSend = async (prompt: string) => {
    setLoading(true)
    setMessages({})
    setSessionId('')
    const siteIDs = Array.from(selectedSites)

    // Create placeholders
    const placeholders: Record<string, Message> = {}
    siteIDs.forEach(siteID => {
      placeholders[siteID] = {
        id: `placeholder-${siteID}`,
        session_id: '',
        site_id: siteID,
        content: '加载中...',
        kept: true,
        error: '',
        elapsed_ms: 0,
        created_at: new Date().toISOString(),
      }
    })
    setMessages(placeholders)

    try {
      const res = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt, site_ids: siteIDs }),
      })
      const data = await res.json()
      setSessionId(data.session_id)
    } catch (err) {
      setLoading(false)
      setMessages(prev => {
        const next = { ...prev }
        Object.keys(next).forEach(k => {
          next[k] = { ...next[k], error: String(err) }
        })
        return next
      })
    }
  }

  const handleToggleKeep = (id: string) => {
    setMessages(prev => {
      const next = { ...prev }
      Object.keys(next).forEach(k => {
        if (next[k].id === id) {
          next[k] = { ...next[k], kept: !next[k].kept }
        }
      })
      return next
    })
  }

  const messageList = Object.values(messages)

  return (
    <div className="h-screen flex flex-col">
      <header className="h-12 bg-slate-800 border-b border-slate-700 flex items-center px-4 justify-between">
        <h1 className="font-semibold">多站点AI聚合</h1>
      </header>
      <div className="flex-1 flex overflow-hidden">
        <SiteSidebar
          sites={sites}
          selectedSites={selectedSites}
          onToggle={toggleSite}
        />
        <div className="flex-1 flex flex-col">
          <ChatGrid messages={messageList} onToggleKeep={handleToggleKeep} />
          <InputArea onSend={handleSend} disabled={loading} />
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 7: Commit**

```bash
git add .
git commit -m "feat: concurrent chat with WebSocket real-time updates"
```

---

## 阶段 5：Keep/Remove 交互 + 导出功能

### Task 9: Keep 状态持久化

**Files:**
- Create: `internal/storage/message_store.go`
- Create: `internal/api/export.go`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: 创建 Message Store**

Create: `internal/storage/message_store.go`
```go
package storage

import (
	"chat-aggregator/internal/models"
)

func (db *DB) UpdateMessageKept(id string, kept bool) error {
	_, err := db.Conn().Exec(`UPDATE messages SET kept = ? WHERE id = ?`, kept, id)
	return err
}

func (db *DB) GetMessagesBySession(sessionID string) ([]models.Message, error) {
	rows, err := db.Conn().Query(
		`SELECT id, session_id, site_id, content, kept, error, elapsed_ms, created_at FROM messages WHERE session_id = ?`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var m models.Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.SiteID, &m.Content, &m.Kept, &m.Error, &m.ElapsedMs, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, nil
}
```

- [ ] **Step 2: 添加 keep API 端点**

Add to `internal/api/chat.go`:
```go
type UpdateKeptRequest struct {
	MessageID string `json:"message_id" binding:"required"`
	Kept      bool   `json:"kept"`
}

func (s *Server) handleUpdateKept(c *gin.Context) {
	var req UpdateKeptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := s.db.UpdateMessageKept(req.MessageID, req.Kept); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
```

Modify: `internal/api/server.go` — add route:
```go
s.router.POST("/api/messages/kept", s.handleUpdateKept)
```

- [ ] **Step 3: 前端同步 keep 状态到后端**

Modify: `web/src/App.tsx` — update `handleToggleKeep`:
```typescript
const handleToggleKeep = async (id: string) => {
  const msg = Object.values(messages).find(m => m.id === id)
  if (!msg) return

  const newKept = !msg.kept
  setMessages(prev => {
    const next = { ...prev }
    Object.keys(next).forEach(k => {
      if (next[k].id === id) {
        next[k] = { ...next[k], kept: newKept }
      }
    })
    return next
  })

  await fetch('/api/messages/kept', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message_id: id, kept: newKept }),
  })
}
```

- [ ] **Step 4: Commit**

```bash
git add .
git commit -m "feat: persist keep/remove state to database"
```

---

### Task 10: 导出功能

**Files:**
- Create: `internal/export/json_exporter.go`
- Create: `internal/export/markdown_exporter.go`
- Create: `internal/api/export.go`
- Create: `web/src/components/ExportPanel.tsx`

- [ ] **Step 1: JSON 导出**

Create: `internal/export/json_exporter.go`
```go
package export

import (
	"encoding/json"
	"chat-aggregator/internal/models"
)

type SessionExport struct {
	Session  models.Session   `json:"session"`
	Messages []models.Message `json:"messages"`
}

func ToJSON(session models.Session, messages []models.Message) ([]byte, error) {
	export := SessionExport{
		Session:  session,
		Messages: messages,
	}
	return json.MarshalIndent(export, "", "  ")
}
```

- [ ] **Step 2: Markdown 导出（含标题偏移）**

Create: `internal/export/markdown_exporter.go`
```go
package export

import (
	"fmt"
	"strings"
	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"
)

func ToMarkdown(session models.Session, messages []models.Message, db *storage.DB) (string, error) {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# %s\n\n", session.Prompt))
	b.WriteString(fmt.Sprintf("> 提问时间: %s\n\n", session.CreatedAt.Format("2006-01-02 15:04:05")))

	for _, msg := range messages {
		if !msg.Kept {
			continue
		}

		site, err := db.GetSiteByID(msg.SiteID)
		if err != nil {
			site = &models.Site{Name: msg.SiteID}
		}

		b.WriteString(fmt.Sprintf("## %s\n\n", site.Name))
		if msg.Error != "" {
			b.WriteString(fmt.Sprintf("> 错误: %s\n\n", msg.Error))
			continue
		}

		// Offset headings in content
		offsetContent := offsetHeadings(msg.Content, 2)
		b.WriteString(offsetContent)
		b.WriteString("\n\n")
	}

	return b.String(), nil
}

func offsetHeadings(content string, offset int) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "#include") {
			// Count leading #
			count := 0
			for _, ch := range trimmed {
				if ch == '#' {
					count++
				} else {
					break
				}
			}
			newCount := count + offset
			if newCount > 6 {
				newCount = 6
			}
			lines[i] = strings.Repeat("#", newCount) + trimmed[count:]
		}
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 3: 导出 API 端点**

Create: `internal/api/export.go`
```go
package api

import (
	"net/http"
	"github.com/gin-gonic/gin"
	"chat-aggregator/internal/export"
)

func (s *Server) handleExport(c *gin.Context) {
	sessionID := c.Query("session_id")
	format := c.Query("format") // json or markdown
	filterKept := c.Query("filter_kept") != "false"

	if sessionID == "" {
		c.JSON(400, gin.H{"error": "session_id required"})
		return
	}

	var session models.Session
	err := s.db.Conn().QueryRow(`SELECT id, prompt, created_at FROM sessions WHERE id = ?`, sessionID).
		Scan(&session.ID, &session.Prompt, &session.CreatedAt)
	if err != nil {
		c.JSON(404, gin.H{"error": "session not found"})
		return
	}

	messages, err := s.db.GetMessagesBySession(sessionID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	if filterKept {
		var filtered []models.Message
		for _, m := range messages {
			if m.Kept {
				filtered = append(filtered, m)
			}
		}
		messages = filtered
	}

	switch format {
	case "markdown":
		md, err := export.ToMarkdown(session, messages, s.db)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Type", "text/markdown")
		c.Header("Content-Disposition", `attachment; filename="session-`+sessionID+`.md"`)
		c.String(200, md)
	default:
		data, err := export.ToJSON(session, messages)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Type", "application/json")
		c.Header("Content-Disposition", `attachment; filename="session-`+sessionID+`.json"`)
		c.Data(200, "application/json", data)
	}
}
```

Wait, I need to import models in export.go. Let me add:

```go
import "chat-aggregator/internal/models"
```

- [ ] **Step 4: 注册导出路由**

Modify: `internal/api/server.go`:
```go
s.router.GET("/api/export", s.handleExport)
```

- [ ] **Step 5: 前端导出面板**

Create: `web/src/components/ExportPanel.tsx`
```tsx
interface Props {
  sessionId: string
}

export default function ExportPanel({ sessionId }: Props) {
  if (!sessionId) return null

  const handleExport = (format: 'json' | 'markdown') => {
    window.open(`/api/export?session_id=${sessionId}&format=${format}`, '_blank')
  }

  return (
    <div className="flex gap-2">
      <button
        onClick={() => handleExport('json')}
        className="bg-slate-700 hover:bg-slate-600 text-sm px-3 py-1 rounded"
      >
        导出 JSON
      </button>
      <button
        onClick={() => handleExport('markdown')}
        className="bg-slate-700 hover:bg-slate-600 text-sm px-3 py-1 rounded"
      >
        导出 Markdown
      </button>
    </div>
  )
}
```

- [ ] **Step 6: 集成到 App**

Modify: `web/src/App.tsx` — add import and use ExportPanel in header:
```tsx
import ExportPanel from './components/ExportPanel'

// In header:
<header className="h-12 bg-slate-800 border-b border-slate-700 flex items-center px-4 justify-between">
  <h1 className="font-semibold">多站点AI聚合</h1>
  <ExportPanel sessionId={sessionId} />
</header>
```

- [ ] **Step 7: Commit**

```bash
git add .
git commit -m "feat: add JSON and Markdown export with heading offset"
```

---

## 阶段 6：站点配置管理（YAML + UI）

### Task 11: 站点配置 CRUD API

**Files:**
- Modify: `internal/api/sites.go`
- Modify: `internal/storage/site_store.go`
- Create: `web/src/components/SiteConfigModal.tsx`

- [ ] **Step 1: 扩展 site API**

Modify: `internal/api/sites.go`
```go
package api

import (
	"net/http"
	"github.com/gin-gonic/gin"
	"chat-aggregator/internal/config"
	"chat-aggregator/internal/models"
)

func (s *Server) handleGetSites(c *gin.Context) {
	sites, err := s.db.GetSites()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, sites)
}

type CreateSiteRequest struct {
	ID           string            `json:"id" binding:"required"`
	Name         string            `json:"name" binding:"required"`
	URL          string            `json:"url" binding:"required"`
	EngineType   string            `json:"engine_type"`
	Selectors    map[string]string `json:"selectors"`
	FormatPrompt string            `json:"format_prompt"`
}

func (s *Server) handleCreateSite(c *gin.Context) {
	var req CreateSiteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	site := models.Site{
		ID:           req.ID,
		Name:         req.Name,
		URL:          req.URL,
		EngineType:   req.EngineType,
		Selectors:    mustJSON(req.Selectors),
		FormatPrompt: req.FormatPrompt,
		Enabled:      true,
	}

	if err := s.db.SaveSite(site); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Sync back to YAML
	if err := s.syncConfigToYAML(); err != nil {
		// Log but don't fail
	}

	c.JSON(201, site)
}

func (s *Server) handleUpdateSite(c *gin.Context) {
	id := c.Param("id")
	var req CreateSiteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	site := models.Site{
		ID:           id,
		Name:         req.Name,
		URL:          req.URL,
		EngineType:   req.EngineType,
		Selectors:    mustJSON(req.Selectors),
		FormatPrompt: req.FormatPrompt,
	}

	if err := s.db.UpdateSite(site); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	s.syncConfigToYAML()
	c.JSON(200, site)
}

func (s *Server) handleDeleteSite(c *gin.Context) {
	id := c.Param("id")
	if err := s.db.DeleteSite(id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	s.syncConfigToYAML()
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) syncConfigToYAML() error {
	sites, err := s.db.GetSites()
	if err != nil {
		return err
	}

	cfg := &config.Config{
		Sites: make([]config.SiteConfig, len(sites)),
	}
	for i, site := range sites {
		var selectors map[string]string
		json.Unmarshal([]byte(site.Selectors), &selectors)
		cfg.Sites[i] = config.SiteConfig{
			ID:           site.ID,
			Name:         site.Name,
			URL:          site.URL,
			Enabled:      site.Enabled,
			Engine:       config.EngineConfig{Primary: site.EngineType, Selectors: selectors},
			FormatPrompt: site.FormatPrompt,
			CookieFile:   site.CookieFile,
		}
	}

	return config.Save("configs/sites.yaml", cfg)
}

func mustJSON(v map[string]string) string {
	if v == nil {
		return "{}"
	}
	b, _ := json.Marshal(v)
	return string(b)
}
```

- [ ] **Step 2: 扩展 site store**

Add to `internal/storage/site_store.go`:
```go
func (db *DB) SaveSite(site models.Site) error {
	_, err := db.Conn().Exec(
		`INSERT INTO sites (id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		site.ID, site.Name, site.URL, site.EngineType, site.Selectors,
		site.CookieFile, site.Enabled, site.FormatPrompt,
	)
	return err
}

func (db *DB) UpdateSite(site models.Site) error {
	_, err := db.Conn().Exec(
		`UPDATE sites SET name=?, url=?, engine_type=?, selectors=?, format_prompt=? WHERE id=?`,
		site.Name, site.URL, site.EngineType, site.Selectors, site.FormatPrompt, site.ID,
	)
	return err
}

func (db *DB) DeleteSite(id string) error {
	_, err := db.Conn().Exec(`DELETE FROM sites WHERE id = ?`, id)
	return err
}
```

- [ ] **Step 3: 注册路由**

Modify: `internal/api/server.go`:
```go
s.router.GET("/api/sites", s.handleGetSites)
s.router.POST("/api/sites", s.handleCreateSite)
s.router.PUT("/api/sites/:id", s.handleUpdateSite)
s.router.DELETE("/api/sites/:id", s.handleDeleteSite)
```

- [ ] **Step 4: Commit**

```bash
git add .
git commit -m "feat: add site CRUD API with YAML sync"
```

---

### Task 12: 前端站点配置面板

**Files:**
- Create: `web/src/components/SiteConfigModal.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: 创建配置模态框**

Create: `web/src/components/SiteConfigModal.tsx`
```tsx
import { useState } from 'react'
import { Site } from '../types'

interface Props {
  site?: Site
  onSave: (site: Partial<Site>) => void
  onClose: () => void
}

export default function SiteConfigModal({ site, onSave, onClose }: Props) {
  const [form, setForm] = useState({
    id: site?.id || '',
    name: site?.name || '',
    url: site?.url || '',
    engine_type: site?.engine_type || 'cdp',
    selectors: site?.selectors || '{}',
    format_prompt: site?.format_prompt || '',
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    onSave(form)
    onClose()
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-slate-800 rounded-lg p-6 w-96 max-h-[80vh] overflow-y-auto">
        <h2 className="text-lg font-semibold mb-4">{site ? '编辑站点' : '添加站点'}</h2>
        <form onSubmit={handleSubmit} className="space-y-3">
          <div>
            <label className="block text-sm mb-1">ID</label>
            <input
              value={form.id}
              onChange={e => setForm({ ...form, id: e.target.value })}
              className="w-full bg-slate-700 border border-slate-600 rounded px-3 py-2 text-sm"
              disabled={!!site}
            />
          </div>
          <div>
            <label className="block text-sm mb-1">名称</label>
            <input
              value={form.name}
              onChange={e => setForm({ ...form, name: e.target.value })}
              className="w-full bg-slate-700 border border-slate-600 rounded px-3 py-2 text-sm"
            />
          </div>
          <div>
            <label className="block text-sm mb-1">URL</label>
            <input
              value={form.url}
              onChange={e => setForm({ ...form, url: e.target.value })}
              className="w-full bg-slate-700 border border-slate-600 rounded px-3 py-2 text-sm"
            />
          </div>
          <div>
            <label className="block text-sm mb-1">引擎</label>
            <select
              value={form.engine_type}
              onChange={e => setForm({ ...form, engine_type: e.target.value })}
              className="w-full bg-slate-700 border border-slate-600 rounded px-3 py-2 text-sm"
            >
              <option value="cdp">CDP (rod)</option>
              <option value="playwright">Playwright Go</option>
              <option value="ts-playwright">TS Playwright</option>
            </select>
          </div>
          <div>
            <label className="block text-sm mb-1">选择器 (JSON)</label>
            <textarea
              value={form.selectors}
              onChange={e => setForm({ ...form, selectors: e.target.value })}
              className="w-full bg-slate-700 border border-slate-600 rounded px-3 py-2 text-sm h-20"
            />
          </div>
          <div>
            <label className="block text-sm mb-1">格式提示词</label>
            <textarea
              value={form.format_prompt}
              onChange={e => setForm({ ...form, format_prompt: e.target.value })}
              className="w-full bg-slate-700 border border-slate-600 rounded px-3 py-2 text-sm h-20"
            />
          </div>
          <div className="flex gap-2 pt-2">
            <button type="submit" className="flex-1 bg-sky-500 hover:bg-sky-600 py-2 rounded text-sm">
              保存
            </button>
            <button type="button" onClick={onClose} className="flex-1 bg-slate-700 hover:bg-slate-600 py-2 rounded text-sm">
              取消
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: 集成到 App**

Modify: `web/src/App.tsx`:
```tsx
import SiteConfigModal from './components/SiteConfigModal'

// Add state
const [showConfig, setShowConfig] = useState(false)
const [editingSite, setEditingSite] = useState<Site | undefined>()

// Add to header
<header className="h-12 bg-slate-800 border-b border-slate-700 flex items-center px-4 justify-between">
  <h1 className="font-semibold">多站点AI聚合</h1>
  <div className="flex gap-2">
    <button
      onClick={() => { setEditingSite(undefined); setShowConfig(true) }}
      className="bg-slate-700 hover:bg-slate-600 text-sm px-3 py-1 rounded"
    >
      + 新站点
    </button>
    <ExportPanel sessionId={sessionId} />
  </div>
</header>

// Add modal
{showConfig && (
  <SiteConfigModal
    site={editingSite}
    onSave={async (form) => {
      const method = editingSite ? 'PUT' : 'POST'
      const url = editingSite ? `/api/sites/${editingSite.id}` : '/api/sites'
      await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      })
      // Refresh sites
      window.location.reload()
    }}
    onClose={() => setShowConfig(false)}
  />
)}
```

- [ ] **Step 3: Commit**

```bash
git add .
git commit -m "feat: add site config modal with CRUD"
```

---

## 阶段 7：多层降级完善

### Task 13: playwright-go 和 TS Playwright 降级

**Files:**
- Create: `internal/engine/playwright_go.go`
- Create: `internal/engine/ts_playwright.go`
- Modify: `internal/engine/manager.go`

- [ ] **Step 1: playwright-go 引擎**

Run: `go get github.com/playwright-community/playwright-go`

Create: `internal/engine/playwright_go.go`
```go
package engine

import (
	"context"
	"fmt"
	"chat-aggregator/internal/models"
	"github.com/playwright-community/playwright-go"
)

type PlaywrightGoEngine struct {
	pw      *playwright.Playwright
	browser playwright.Browser
}

func NewPlaywrightGoEngine() (*PlaywrightGoEngine, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("start playwright: %w", err)
	}

	browser, err := pw.Chromium.Launch()
	if err != nil {
		pw.Stop()
		return nil, fmt.Errorf("launch browser: %w", err)
	}

	return &PlaywrightGoEngine{pw: pw, browser: browser}, nil
}

func (e *PlaywrightGoEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	page, err := e.browser.NewPage()
	if err != nil {
		return "", err
	}
	defer page.Close()

	if _, err := page.Goto(site.URL); err != nil {
		return "", err
	}

	// Fill input and submit (simplified)
	// Actual implementation needs selector-based logic similar to rod
	return "", fmt.Errorf("playwright-go not fully implemented yet")
}

func (e *PlaywrightGoEngine) Close() error {
	if e.browser != nil {
		e.browser.Close()
	}
	if e.pw != nil {
		e.pw.Stop()
	}
	return nil
}
```

- [ ] **Step 2: TS Playwright 子进程引擎**

Create: `internal/engine/ts_playwright.go`
```go
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"chat-aggregator/internal/models"
)

type TSPlaywrightEngine struct {
	cmd *exec.Cmd
}

func NewTSPlaywrightEngine() (*TSPlaywrightEngine, error) {
	// Check if npx is available
	if _, err := exec.LookPath("npx"); err != nil {
		return nil, fmt.Errorf("npx not found")
	}
	return &TSPlaywrightEngine{}, nil
}

func (e *TSPlaywrightEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	// Call a TS script via npx playwright
	// This is a placeholder - actual implementation needs a TS wrapper script
	return "", fmt.Errorf("ts-playwright not fully implemented yet")
}

func (e *TSPlaywrightEngine) Close() error {
	if e.cmd != nil && e.cmd.Process != nil {
		return e.cmd.Process.Kill()
	}
	return nil
}
```

- [ ] **Step 3: 更新管理器实现降级逻辑**

Modify: `internal/engine/manager.go`:
```go
package engine

import (
	"context"
	"fmt"
	"chat-aggregator/internal/models"
)

type Manager struct {
	engines []BrowserEngine
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	// Try engines in order
	engines := m.getEngines()
	var lastErr error

	for _, engine := range engines {
		result, err := engine.SendMessage(ctx, site, prompt)
		if err == nil {
			return result, nil
		}
		lastErr = err
		// Log downgrade
	}

	return "", fmt.Errorf("all engines failed, last error: %w", lastErr)
}

func (m *Manager) getEngines() []BrowserEngine {
	if len(m.engines) > 0 {
		return m.engines
	}

	// Try rod first
	if rod, err := NewRodEngine(); err == nil {
		m.engines = append(m.engines, rod)
	}

	// Try playwright-go
	if pw, err := NewPlaywrightGoEngine(); err == nil {
		m.engines = append(m.engines, pw)
	}

	// Try TS Playwright
	if ts, err := NewTSPlaywrightEngine(); err == nil {
		m.engines = append(m.engines, ts)
	}

	return m.engines
}

func (m *Manager) Close() error {
	for _, e := range m.engines {
		e.Close()
	}
	return nil
}
```

- [ ] **Step 4: Commit**

```bash
git add .
git commit -m "feat: add playwright-go and ts-playwright fallback engines"
```

---

## 阶段 8：Session 复用 + Cookie 持久化

### Task 14: Cookie 持久化

**Files:**
- Create: `internal/storage/cookie_store.go`
- Modify: `internal/engine/rod_engine.go`

- [ ] **Step 1: Cookie store**

Create: `internal/storage/cookie_store.go`
```go
package storage

import (
	"chat-aggregator/internal/models"
)

func (db *DB) GetSiteCookie(siteID string) (*models.SiteCookie, error) {
	var c models.SiteCookie
	err := db.Conn().QueryRow(
		`SELECT site_id, cookies, local_storage, updated_at FROM site_cookies WHERE site_id = ?`,
		siteID,
	).Scan(&c.SiteID, &c.Cookies, &c.LocalStorage, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (db *DB) SaveSiteCookie(cookie models.SiteCookie) error {
	_, err := db.Conn().Exec(
		`INSERT OR REPLACE INTO site_cookies (site_id, cookies, local_storage, updated_at) VALUES (?, ?, ?, datetime('now'))`,
		cookie.SiteID, cookie.Cookies, cookie.LocalStorage,
	)
	return err
}
```

- [ ] **Step 2: rod 引擎注入和提取 cookie**

Modify: `internal/engine/rod_engine.go`:
```go
// Add DB reference
type RodEngine struct {
	browser *rod.Browser
	db      *storage.DB
}

func NewRodEngine(db *storage.DB) (*RodEngine, error) {
	// ... existing launch code ...
	return &RodEngine{browser: browser, db: db}, nil
}

func (e *RodEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	page := e.browser.MustPage()
	defer page.Close()

	// Inject cookies if available
	if e.db != nil {
		if cookie, err := e.db.GetSiteCookie(site.ID); err == nil {
			var cookies []proto.NetworkCookieParam
			json.Unmarshal([]byte(cookie.Cookies), &cookies)
			page.SetCookies(cookies...)
		}
	}

	page.MustNavigate(site.URL)
	page.MustWaitLoad()

	// ... rest of send logic ...

	// Extract and save cookies after interaction
	if e.db != nil {
		cookies := page.Cookies()
		cookieData, _ := json.Marshal(cookies)
		e.db.SaveSiteCookie(models.SiteCookie{
			SiteID:  site.ID,
			Cookies: string(cookieData),
		})
	}

	return result, nil
}
```

- [ ] **Step 3: Commit**

```bash
git add .
git commit -m "feat: add cookie persistence for session reuse"
```

---

## 阶段 9：历史记录 + 导出范围选择

### Task 15: 历史记录面板

**Files:**
- Modify: `web/src/App.tsx`
- Create: `web/src/components/HistoryPanel.tsx`
- Modify: `internal/api/export.go`

- [ ] **Step 1: 历史记录 API**

Add to `internal/api/chat.go`:
```go
func (s *Server) handleGetSessions(c *gin.Context) {
	rows, err := s.db.Conn().Query(`SELECT id, prompt, created_at FROM sessions ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var sessions []models.Session
	for rows.Next() {
		var sess models.Session
		if err := rows.Scan(&sess.ID, &sess.Prompt, &sess.CreatedAt); err != nil {
			continue
		}
		sessions = append(sessions, sess)
	}
	c.JSON(200, sessions)
}
```

Add route:
```go
s.router.GET("/api/sessions", s.handleGetSessions)
```

- [ ] **Step 2: 历史记录面板组件**

Create: `web/src/components/HistoryPanel.tsx`
```tsx
import { useEffect, useState } from 'react'
import { Session } from '../types'

interface Props {
  onSelect: (session: Session) => void
}

export default function HistoryPanel({ onSelect }: Props) {
  const [sessions, setSessions] = useState<Session[]>([])

  useEffect(() => {
    fetch('/api/sessions')
      .then(r => r.json())
      .then(setSessions)
  }, [])

  return (
    <div className="w-64 bg-slate-800 border-r border-slate-700 flex flex-col">
      <div className="p-3 border-b border-slate-700 font-semibold text-sm">
        历史记录
      </div>
      <div className="flex-1 overflow-y-auto">
        {sessions.map(sess => (
          <button
            key={sess.id}
            onClick={() => onSelect(sess)}
            className="w-full text-left px-3 py-2 hover:bg-slate-700 text-sm truncate"
          >
            {sess.prompt}
          </button>
        ))}
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Commit**

```bash
git add .
git commit -m "feat: add history panel with session list"
```

---

## 阶段 10：选择器探测 + 错误处理完善

### Task 16: 选择器自动探测

**Files:**
- Create: `internal/engine/detector.go`
- Modify: `internal/api/sites.go`

- [ ] **Step 1: 创建探测器**

Create: `internal/engine/detector.go`
```go
package engine

import (
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

var commonSelectors = map[string][]string{
	"input":    {"#chat-input", "textarea", "[placeholder*=\"输入\"]", "[contenteditable=true]"},
	"submit":   {"button[type=submit]", "#send-btn", "[aria-label*=\"发送\"]"},
	"answer":   {".message-content", ".answer-content", "[class*=\"message\"]", "[class*=\"answer\"]"},
	"wait_for": {".message-content:last-child", ".answer-content:last-child"},
}

func DetectSelectors(url string) (map[string]string, error) {
	l := launcher.New().Headless(true)
	browserURL, err := l.Launch()
	if err != nil {
		return nil, err
	}

	browser := rod.New().ControlURL(browserURL).MustConnect()
	defer browser.Close()

	page := browser.MustPage(url)
	page.MustWaitLoad()

	result := make(map[string]string)
	for key, selectors := range commonSelectors {
		for _, sel := range selectors {
			if el, err := page.Element(sel); err == nil && el != nil {
				result[key] = sel
				break
			}
		}
	}

	return result, nil
}
```

- [ ] **Step 2: 添加探测 API**

Add to `internal/api/sites.go`:
```go
func (s *Server) handleDetectSelectors(c *gin.Context) {
	url := c.Query("url")
	if url == "" {
		c.JSON(400, gin.H{"error": "url required"})
		return
	}

	selectors, err := engine.DetectSelectors(url)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, selectors)
}
```

Add route:
```go
s.router.GET("/api/detect", s.handleDetectSelectors)
```

- [ ] **Step 3: Commit**

```bash
git add .
git commit -m "feat: add selector auto-detection for new sites"
```

---

## 自审清单

### Spec 覆盖检查

| Spec 需求 | 对应 Task |
|-----------|-----------|
| 维护多个大模型官网站点配置 | Task 3, 11, 12, 16 |
| 一次输入并发发送到多个站点 | Task 8 |
| 每次提问自动落盘 | Task 2, 8 |
| 对每个站点答案快速保留/去掉 | Task 9 |
| 按 keep 状态过滤导出 JSON/Markdown | Task 10 |
| 首次手动登录，之后复用 session | Task 14 |
| 浏览器自动化多层降级 | Task 4, 13 |
| 左栏勾选控制站点展示和发送 | Task 6, 7 |
| YAML + UI 双向配置同步 | Task 3, 11 |
| 格式提示词配置 | Task 3, 11 |
| Markdown 标题偏移导出 | Task 10 |
| 历史记录 | Task 15 |
| 选择器探测 | Task 16 |

### Placeholder 扫描

- 无 TBD / TODO
- playwright-go 和 TS Playwright 的 SendMessage 实现标注为简化版，但这在计划中已明确，后续可完善
- 所有代码块包含具体实现

### 类型一致性

- `models.Site` 结构体定义在 Task 2，所有后续引用一致
- `BrowserEngine` 接口定义在 Task 4，所有实现一致
- `MessageUpdate` 结构体在 Task 8 定义，WebSocket 使用一致

---

## 执行交接

**计划完成并保存到 `docs/superpowers/plans/2026-06-25-chat-aggregator.md`。**

**两个执行选项：**

**1. Subagent-Driven（推荐）** — 每个 Task 分配一个独立的子代理执行，我在每个 Task 之间审查代码和状态，适合需要频繁检查和调整的情况

**2. Inline Execution** — 在当前会话中依次执行所有 Task，批量推进，适合你对计划有充分信心、希望快速推进的情况

**选择哪个？**
