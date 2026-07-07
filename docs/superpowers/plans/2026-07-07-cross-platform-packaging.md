# 跨平台单一可执行文件打包实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 生成跨平台可执行文件，用户双击即可使用，集成系统托盘、自动打开浏览器、端口自动检测。

**Architecture:** 使用 systray 库实现系统托盘，修改 Server 支持端口检测，main.go 集成托盘启动流程，Makefile 新增交叉编译目标。

**Tech Stack:** Go 1.23, github.com/getlantern/systray, gin

---

## 文件结构

| 文件 | 职责 |
|------|------|
| `cmd/server/main.go` | 入口：创建目录、端口检测、启动服务、集成托盘 |
| `internal/api/server.go` | HTTP 服务器：端口检测逻辑、优雅关闭 |
| `internal/systrayapp/systray.go` | 新增：托盘图标、菜单、浏览器打开 |
| `assets/icon.ico` | 新增：托盘图标文件 |
| `Makefile` | 更新：新增 cross-build 目标 |

---

### Task 1: 添加 systray 依赖

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 安装 systray 库**

```bash
cd D:\Desktopfile\chores\KnowledgeClip
go get github.com/getlantern/systray
```

Expected: go.mod 和 go.sum 更新，包含 systray 依赖

- [ ] **Step 2: 验证依赖安装成功**

```bash
go mod tidy
```

Expected: 无报错

- [ ] **Step 3: 提交**

```bash
git add go.mod go.sum
git commit -m "chore(deps): add systray library for system tray support"
```

---

### Task 2: 创建托盘图标文件

**Files:**
- Create: `assets/icon.ico`

- [ ] **Step 1: 创建 assets 目录**

```bash
mkdir -p D:\Desktopfile\chores\KnowledgeClip\assets
```

- [ ] **Step 2: 创建简单的托盘图标**

使用 Go 代码生成一个简单的 16x16 ICO 文件。创建临时脚本 `gen_icon.go`：

```go
//go:build ignore

package main

import (
	"encoding/base64"
	"os"
)

// 16x16 ICO 文件的 base64 编码（蓝色圆形图标）
const iconBase64 = `AAABAAEAEBAAAAEAIABoBAAAFgAAACgAAAAQAAAAIAAAAAEAIAAAAAAAAAQAABMLAAATCwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A/39/AP9/fwD/f38A/39/AP9/fwD///8A////AP///wD///8A////AP///wD///8A////AP///wD/f38A/5+fAP+fnwD/n58A/5+fAP+fnwD/f38A////AP///wD///8A////AP///wD///8A////AP9/fwD/n58A/6KgAP+ioAD/oqAA/6KgAP+ioAD/n58A/39/AP///wD///8A////AP///wD///8A/39/AP+fnwD/oqAA/6KgAP+ioAD/oqAA/6KgAP+ioAD/n58A/39/AP///wD///8A////AP///wD/f38A/5+fAP+ioAD/oqAA/6KgAP+ioAD/oqAA/6KgAP+ioAD/n58A/39/AP///wD///8A////AP9/fwD/n58A/6KgAP+ioAD/oqAA/6KgAP+ioAD/oqAA/6KgAP+ioAD/n58A/39/AP///wD///8A////AP9/fwD/n58A/6KgAP+ioAD/oqAA/6KgAP+ioAD/oqAA/6KgAP+ioAD/n58A/39/AP///wD///8A////AP9/fwD/n58A/6KgAP+ioAD/oqAA/6KgAP+ioAD/oqAA/6KgAP+ioAD/n58A/39/AP///wD///8A////AP///wD/f38A/5+fAP+ioAD/oqAA/6KgAP+ioAD/oqAA/6KgAP+fnwD/f38A////AP///wD///8A////AP///wD///8A/39/AP+fnwD/n58A/5+fAP+fnwD/n58A/5+fAP+fnwD/f38A////AP///wD///8A////AP///wD///8A////AP///wD/f38A/39/AP9/fwD/f38A/39/AP9/fwD/f38A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A////AP///wD///8A//8AAP//AACB/wAA//8AAAAAAAD//wAAAAAAAAD//wAAAAAAAAD//wAAAAAAAAD//wAAAAAAAAD//wAAAAAAAAD//wAAAAAAAAD//wAAAAAAAAD//wAA//8AAA==`

func main() {
	data, err := base64.StdEncoding.DecodeString(iconBase64)
	if err != nil {
		panic(err)
	}
	err = os.WriteFile("assets/icon.ico", data, 0644)
	if err != nil {
		panic(err)
	}
}
```

运行脚本：

```bash
cd D:\Desktopfile\chores\KnowledgeClip
go run gen_icon.go
rm gen_icon.go
```

Expected: `assets/icon.ico` 文件创建成功，约 1KB

- [ ] **Step 3: 验证图标文件**

```bash
ls -la assets/icon.ico
```

Expected: 文件存在，大小约 1000 字节

- [ ] **Step 4: 提交**

```bash
git add assets/icon.ico
git commit -m "chore(assets): add system tray icon"
```

---

### Task 3: 创建托盘模块

**Files:**
- Create: `internal/systrayapp/systray.go`

- [ ] **Step 1: 创建目录和文件**

```bash
mkdir -p D:\Desktopfile\chores\KnowledgeClip\internal\systrayapp
```

- [ ] **Step 2: 编写托盘模块代码**

```go
package systrayapp

import (
	_ "embed"
	"fmt"
	"log"
	"os/exec"
	"runtime"

	"github.com/getlantern/systray"
)

//go:embed icon.ico
var iconData []byte

var actualPort int

// SetPort 设置实际使用的端口，用于显示在托盘菜单标题中
func SetPort(port int) {
	actualPort = port
}

// Run 启动系统托盘
func Run(onExit func()) {
	systray.Run(func() {
		onReady()
	}, onExit)
}

func onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle(fmt.Sprintf("KnowledgeClip (端口: %d)", actualPort))
	systray.SetTooltip("KnowledgeClip - 多站点聊天聚合器")

	mOpen := systray.AddMenuItem("打开界面", "在浏览器中打开界面")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "关闭程序")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				openBrowser(actualPort)
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	// 启动后自动打开浏览器
	go func() {
		openBrowser(actualPort)
	}()
}

func openBrowser(port int) {
	url := fmt.Sprintf("http://localhost:%d", port)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}

	if err := cmd.Start(); err != nil {
		log.Printf("failed to open browser: %v", err)
	}
}
```

- [ ] **Step 3: 移动图标文件到 systrayapp 目录**

由于 `//go:embed` 需要嵌入同一包内的文件，将图标移到 systrayapp 目录：

```bash
mv D:\Desktopfile\chores\KnowledgeClip\assets\icon.ico D:\Desktopfile\chores\KnowledgeClip\internal\systrayapp\icon.ico
rmdir D:\Desktopfile\chores\KnowledgeClip\assets
```

- [ ] **Step 4: 验证编译通过**

```bash
cd D:\Desktopfile\chores\KnowledgeClip
go build ./internal/systrayapp
```

Expected: 无报错

- [ ] **Step 5: 提交**

```bash
git add internal/systrayapp/
git commit -m "feat(systray): add system tray module with browser open support"
```

---

### Task 4: 修改 Server 支持端口检测和优雅关闭

**Files:**
- Modify: `internal/api/server.go`

- [ ] **Step 1: 修改 Server 结构体和方法**

修改 `internal/api/server.go`，添加端口检测和优雅关闭支持：

```go
package api

import (
	"chat-aggregator/internal/engine"
	"chat-aggregator/internal/storage"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type Server struct {
	router  *gin.Engine
	db      *storage.DB
	hub     *Hub
	manager *engine.Manager
	server  *http.Server
	port    int
}

// NewServer 创建服务器实例
func NewServer(db *storage.DB, manager *engine.Manager) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	s := &Server{router: r, db: db, hub: NewHub(), manager: manager}
	s.setupRoutes()
	setupStatic(r)

	return s
}

func (s *Server) setupRoutes() {
	s.router.GET("/api/health", s.handleHealth)
	s.router.GET("/api/sites", s.handleGetSites)
	s.router.POST("/api/sites", s.handleCreateSite)
	s.router.PUT("/api/sites/:id", s.handleUpdateSite)
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

// Run 启动服务器，尝试从 basePort 开始绑定，最多尝试 maxAttempts 次
// 返回实际绑定的端口
func (s *Server) Run(basePort int) (int, error) {
	maxAttempts := 10
	actualPort := basePort

	for i := 0; i < maxAttempts; i++ {
		actualPort = basePort + i
		addr := ":" + strconv.Itoa(actualPort)

		s.server = &http.Server{
			Addr:    addr,
			Handler: s.router,
		}

		// 尝试启动
		err := s.server.ListenAndServe()
		if err == http.ErrServerClosed {
			// 服务器正常关闭
			return actualPort, nil
		}
		if err != nil {
			// 检查是否是端口占用错误
			if isAddrInUse(err) {
				fmt.Printf("端口 %d 已被占用，尝试下一个端口...\n", actualPort)
				continue
			}
			return 0, err
		}
	}

	return 0, fmt.Errorf("无法找到可用端口 (尝试了 %d-%d)", basePort, basePort+maxAttempts-1)
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown() {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.server.Shutdown(ctx)
	}
}

// isAddrInUse 检查错误是否是端口占用
func isAddrInUse(err error) bool {
	return err != nil && (err.Error() == "bind: address already in use" ||
		err.Error() == "listen tcp :"+strconv.Itoa(0)+": bind: address already in use")
}
```

- [ ] **Step 2: 验证编译通过**

```bash
cd D:\Desktopfile\chores\KnowledgeClip
go build ./internal/api
```

Expected: 无报错

- [ ] **Step 3: 提交**

```bash
git add internal/api/server.go
git commit -m "refactor(api): add port detection and graceful shutdown support"
```

---

### Task 5: 修改 main.go 集成托盘和目录创建

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: 重写 main.go**

```go
package main

import (
	"chat-aggregator/internal/api"
	"chat-aggregator/internal/config"
	"chat-aggregator/internal/engine"
	"chat-aggregator/internal/storage"
	"chat-aggregator/internal/systrayapp"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	// 1. 创建必要的目录
	createDirectories()

	// 2. 初始化数据库
	db, err := storage.NewDB("data/knowledgeclip.db")
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	// 3. 加载配置
	cfg, err := config.Load("configs/sites.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 4. 同步站点到数据库
	sites := cfg.ToModels()
	if err := storage.SyncSites(db, sites); err != nil {
		log.Fatalf("sync sites: %v", err)
	}

	// 5. 创建引擎管理器
	manager := engine.NewManager(db)

	// 6. 创建服务器
	server := api.NewServer(db, manager)

	// 7. 启动服务器（在 goroutine 中）
	serverReady := make(chan int, 1)
	serverErr := make(chan error, 1)

	go func() {
		port, err := server.Run(8080)
		if err != nil {
			serverErr <- err
			return
		}
		serverReady <- port
	}()

	// 8. 等待服务器启动或出错
	var actualPort int
	select {
	case actualPort = <-serverReady:
		fmt.Printf("服务器启动在端口 %d\n", actualPort)
	case err := <-serverErr:
		log.Fatalf("start server: %v", err)
	}

	// 9. 设置托盘端口并启动托盘
	systrayapp.SetPort(actualPort)

	// 10. 设置退出处理
	exitChan := make(chan struct{})
	go func() {
		// 等待 Ctrl+C（开发模式）
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		close(exitChan)
	}()

	// 11. 启动托盘（阻塞直到退出）
	systrayapp.Run(func() {
		// 托盘退出回调
		fmt.Println("正在关闭服务...")
		server.Shutdown()
		manager.Close()
		db.Close()
		fmt.Println("服务已关闭")
	})
}

// createDirectories 创建必要的目录
func createDirectories() {
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

	// 如果 configs/sites.yaml 不存在，创建默认配置
	configPath := filepath.Join("configs", "sites.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := `sites: []
`
		err := os.WriteFile(configPath, []byte(defaultConfig), 0644)
		if err != nil {
			log.Fatalf("create default config: %v", err)
		}
		fmt.Println("已创建默认配置文件: configs/sites.yaml")
	}
}
```

- [ ] **Step 2: 验证编译通过**

```bash
cd D:\Desktopfile\chores\KnowledgeClip
go build ./cmd/server
```

Expected: 无报错

- [ ] **Step 3: 提交**

```bash
git add cmd/server/main.go
git commit -m "feat(main): integrate systray and auto-create directories"
```

---

### Task 6: 更新 Makefile 添加跨平台编译

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: 更新 Makefile**

```makefile
.PHONY: build run dev clean cross-build

build:
	go build -o bin/server.exe cmd/server/main.go

run: build
	.\bin\server.exe

dev:
	go run cmd/server/main.go

clean:
	rm -f bin/server.exe
	rm -rf dist/

# 跨平台编译
cross-build:
	cd web && npm run build
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o dist/KnowledgeClip-windows.exe cmd/server/main.go
	GOOS=darwin GOARCH=amd64 go build -o dist/KnowledgeClip-macos-intel cmd/server/main.go
	GOOS=darwin GOARCH=arm64 go build -o dist/KnowledgeClip-macos-arm64 cmd/server/main.go
	@echo "构建完成，产物在 dist/ 目录"

# 仅编译 Windows 版本（当前开发环境）
build-windows:
	cd web && npm run build
	go build -ldflags "-H windowsgui" -o dist/KnowledgeClip-windows.exe cmd/server/main.go
```

- [ ] **Step 2: 提交**

```bash
git add Makefile
git commit -m "feat(build): add cross-platform build targets"
```

---

### Task 7: 本地测试 Windows 版本

**Files:**
- None (测试验证)

- [ ] **Step 1: 构建前端**

```bash
cd D:\Desktopfile\chores\KnowledgeClip\web
npm run build
```

Expected: 前端构建成功，产物写入 `internal/api/static/`

- [ ] **Step 2: 构建 Windows 可执行文件**

```bash
cd D:\Desktopfile\chores\KnowledgeClip
go build -ldflags "-H windowsgui" -o dist/KnowledgeClip-windows.exe cmd/server/main.go
```

Expected: `dist/KnowledgeClip-windows.exe` 生成成功

- [ ] **Step 3: 手动测试运行**

1. 双击 `dist/KnowledgeClip-windows.exe`
2. 验证：
   - 系统托盘出现图标
   - 浏览器自动打开 `http://localhost:8080`
   - 界面正常显示
   - 右键托盘图标显示菜单
   - 点击"打开界面"可以再次打开浏览器
   - 点击"退出"可以关闭程序

- [ ] **Step 4: 测试端口冲突场景**

1. 先启动一个服务占用 8080 端口
2. 再运行 `dist/KnowledgeClip-windows.exe`
3. 验证：
   - 程序自动尝试 8081 端口
   - 托盘标题显示正确的端口
   - 浏览器打开正确的端口

---

### Task 8: 更新 CLAUDE.md 文档

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: 在 CLAUDE.md 中添加构建说明**

在 `## 构建与运行` 部分添加：

```markdown
### 跨平台编译

生成可发布的可执行文件：

```bash
make cross-build    # 编译 Windows + macOS (Intel/ARM) 版本
make build-windows  # 仅编译 Windows 版本
```

产物输出到 `dist/` 目录：
- `KnowledgeClip-windows.exe` — Windows 版本（双击运行，带系统托盘）
- `KnowledgeClip-macos-intel` — macOS Intel 版本
- `KnowledgeClip-macos-arm64` — macOS Apple Silicon 版本

首次运行会自动创建 `configs/`、`data/`、`.browser-data/` 目录和默认配置文件。
```

- [ ] **Step 2: 提交**

```bash
git add CLAUDE.md
git commit -m "docs: add cross-platform build instructions"
```

---

## Self-Review Checklist

**1. Spec coverage:**
- [x] 系统托盘 — Task 3
- [x] 自动打开浏览器 — Task 3 (systray.go openBrowser)
- [x] 端口检测与自动切换 — Task 4 (server.go Run)
- [x] 数据目录创建 — Task 5 (main.go createDirectories)
- [x] 交叉编译 — Task 6 (Makefile cross-build)
- [x] 托盘图标 — Task 2, Task 3

**2. Placeholder scan:** 无 TBD、TODO、"implement later" 等占位符

**3. Type consistency:**
- `server.Run(basePort int) (int, error)` 返回实际端口
- `systrayapp.SetPort(port int)` 接收端口
- main.go 中 `actualPort` 变量类型一致

**4. Spec requirement gaps:**
- macOS 首次运行 Gatekeeper 授权提示 — 在 CLAUDE.md 文档中说明（Task 8 中补充）

---

## 修正：补充 macOS Gatekeeper 说明

在 Task 8 中补充文档：

```markdown
**macOS 用户注意：**
首次运行可能被 macOS Gatekeeper 阻止。解决方法：
1. 右键点击文件 → 选择"打开" → 在弹出对话框中点击"打开"
2. 或在"系统设置" → "隐私与安全性"中允许运行
```
