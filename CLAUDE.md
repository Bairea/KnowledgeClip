# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目

多站点大模型官网站点聊天聚合器（Qwen / Kimi / DeepSeek 等）。一次输入并发发送到多个已启用的站点，UI 中按站点网格展示回答，可对每条回答做 keep/remove，支持按 keep 过滤导出 JSON 或 Markdown。

技术栈：Go 1.23 (gin + go-rod + playwright-go) + React 18 + Vite + SQLite（`modernc.org/sqlite` 纯 Go 驱动）。单二进制部署：Go 服务把构建后的前端静态文件 `embed` 内嵌并直接托管。

## 构建与运行

后端（`Makefile`，Windows + bash）：

```bash
make build      # 编译到 bin/server.exe
make run        # 编译并运行
make dev        # go run cmd/server/main.go
```

前端（`web/`）：

```bash
cd web
npm install
npm run dev     # 开发服务器，代理 /api 与 /ws 到 http://localhost:8080
npm run build   # tsc + vite build，输出到 ../internal/api/static/
```

后端默认监听 `:8080`。`npm run build` 会 `emptyOutDir: true` 清空 `internal/api/static`，构建后必须重新 `go build`，否则旧二进制仍服务旧资源。

**构建顺序陷阱**：`internal/api/static.go` 用了 `//go:embed all:static` 把前端产物打进二进制。`go build` 时只会 embed **当时目录里**的产物。所以改完前端代码后：
1. **先**跑 `cd web && npm run build`（写入新 hash 的 `index-*.js`/`index-*.css` 到 `internal/api/static/`）
2. **再**跑 `go build`（embed 这些新文件）

反过来的话，binary 会被 embed 进 npm run build 之前还在的旧文件，而 `emptyOutDir: true` 会把旧文件删掉——结果是 binary 服务的 index.html 引用一个目录里已经不存在、但 binary 内部仍 embed 着的旧 asset hash。验证方法：跑完后 `curl -s http://localhost:8080/ | grep -oE 'index-[A-Za-z0-9_-]+\.js'` 应该和 `ls internal/api/static/assets/` 列出的文件同名。

**重启顺序陷阱**：Go 编译的二进制在 Windows 上不能被运行中的进程覆盖替换（会报 sharing violation）。改完代码后必须：先 `TaskStop` 旧进程 → 再 `go build` → 再启动新进程。如果只 build 不停进程，新代码写不进 binary。

无测试套件；没有 lint / format 配置（仅靠 `gofmt` 与 `tsc --noEmit` 兜底）。

## 后端架构（`cmd/server`, `internal/`）

`main.go` 是唯一的组装入口：开 DB → 加载 `configs/sites.yaml` → 用 `storage.SyncSites` 把 YAML upsert 进 SQLite → 创建 `engine.Manager` → 启动 `api.Server`。所有包都是直接函数调用，**没有 DI 容器**，全局状态通过 `main` 注入。

```
cmd/server/main.go
  └─ internal/api         (Gin 路由 + WebSocket Hub)
  └─ internal/engine      (浏览器引擎管理器 + 三层降级实现)
  └─ internal/storage     (SQLite 仓库函数，按表分文件)
  └─ internal/config      (sites.yaml 加载/保存)
  └─ internal/models      (Site / Session / Message / SiteCookie)
  └─ internal/export      (JSON / Markdown 序列化)
```

### `internal/api`

按职责拆文件而不是按层拆：
- `server.go`：路由表（`/api/health`、`/api/sites`、`/api/chat`、`/api/sessions[/:id/messages]`、`/api/messages/kept`、`/api/export`、`/api/detect`、`/ws`）。
- `chat.go`：核心流程——`/api/chat` 创建 session，**先返回 `session_id` 再后台 goroutine 并发发请求**，每个站点完成时 `hub.Broadcast(MessageUpdate{Type:"message", ...})`，所有完成再广播 `Type:"complete"`。
- `websocket.go`：`Hub` 用 `sync.RWMutex` 保护 `map[*websocket.Conn]bool`，`Broadcast` 在 `WriteJSON` 失败时直接 `delete`，所以客户端断连无需手动 `Remove`。
- `sites.go`：CRUD 后必须调用 `syncConfigToYAML()` 把 DB 反向写回 YAML（**双向同步**）。
- `export.go`：`/api/export?session_id=...&format=json|markdown&filter_kept=true`。

### `internal/engine`（三层降级）

`Manager.engines` 依次尝试，第一个成功即返回。`SendMessage` 统一签名 `func(ctx, site, prompt) (string, error)`。

| 顺序 | 文件 | 引擎 | 状态 |
|------|------|------|------|
| 1 | `rod_engine.go` | go-rod (Chrome DevTools Protocol) | 完整实现，含 cookie 注入/持久化 |
| 2 | `playwright_go.go` | `playwright-community/playwright-go` | 完整实现，无 cookie 持久化 |
| 3 | `ts_playwright.go` | 调 npx 子进程 | **未实现**（`SendMessage` 返回 `not yet implemented`） |

`NewManager` 用 `_ = errs` 静默吞掉子引擎初始化错误，不要照搬——按全局约束应该删掉。

`engine.DetectSelectors(url)` 是独立的子流程，给 SiteConfigModal 的 "Auto Detect" 按钮用，启动一次性 headless rod 试常见 selector 候选。

### `internal/storage`

仓库函数按表分文件：`site_store.go` / `session_store.go` / `message_store.go` / `cookie_store.go`，`db.go` 负责 `CREATE TABLE IF NOT EXISTS` schema 和 `*sql.DB` 句柄。`Enabled`/`Kept` 在 DB 中是 INTEGER，在 model 中是 bool，`Scan` 时手动转换。

### `internal/config`

`Config{Global, Sites}`，每个站点的 `Engine.Selectors` 是 `map[string]string`，`ToModels()` 把它 marshal 成 JSON 字符串塞进 `models.Site.Selectors`（DB 里的 `selectors` 列就是 JSON 文本）。`Save`/`Load` 直接走 `gopkg.in/yaml.v3`。

## 前端架构（`web/src/`）

单页 React，**全部状态都在 `App.tsx`**，子组件只接收 props 回调，没有 Context、没有 Redux。

```
App.tsx
 ├─ SiteSidebar        站点勾选 + 编辑入口
 ├─ HistoryPanel       GET /api/sessions 列表（最近 50 条）
 ├─ ChatGrid / MessageCard / KeepSwitch   回答网格
 ├─ InputArea          发送 prompt
 ├─ ExportPanel        当前 session 存在时显示，window.open JSON/MD
 └─ SiteConfigModal    新建/编辑站点，含 /api/detect 自动探测
hooks: useSites, useWebSocket
types: Site, Session, Message
```

`useWebSocket` 用 `ref` 持有最新 `onMessage` 避免重连：依赖数组空 `[]`，断线时 `setTimeout(window.location.reload, 3000)`——前端没有重连退避。

WebSocket 消息协议与 `internal/api/websocket.go` 的 `MessageUpdate` 严格一致：`type` ∈ {`message`, `complete`}，前端用 `id = session_id-site_id` 去重。

## 关键约定

- **站点配置双向同步**：启动时 YAML → DB（`SyncSites`），UI 增删改后 DB → YAML（`syncConfigToYAML`）。两套配置互为缓存。改站点只走 API，不要直接编辑 YAML 再期待运行中生效。
- **Selectors 是 JSON 字符串**：在 DB 列里是 `TEXT`（`Site.Selectors` 字段类型是 `string`），通过 `json.Unmarshal` 成 `Selectors{Input, Submit, Answer, WaitFor}`。`detector.go` 给出的 `wait_for` 默认是 `<answer_selector>:last-child`。
- **Cookie 持久化**：仅 `RodEngine` 实现（`internal/storage/cookie_store.go`），存的是 `proto.NetworkCookieParam` 的 JSON。Playwright 引擎不持久化，跨引擎切换会丢登录态。
- **`/api/chat` 立即返回 session_id**：所有长任务在 goroutine 里跑，HTTP 响应不等所有站点完成。客户端通过 WebSocket 收结果，落地逻辑见 `chat.go:71-113`。
- **端口**：后端 `:8080`；Vite dev 代理把 `/api` 和 `/ws` 转发到 `http://localhost:8080`（见 `web/vite.config.ts`）。
- **导出过滤**：`/api/export` 默认 `filter_kept=true`；要导出全部答案必须显式传 `filter_kept=false`。
- **EngineType 枚举**：DB 里的 `engine_type` 字段是 `cdp` / `playwright` / `ts-playwright`（连字符），前端下拉框硬编码同值，新增引擎要同步两处。
- **ts-playwright 占位**：`TSPlaywrightEngine` 只检查 `npx` 是否存在，`SendMessage` 仍返回错误——不要在功能代码里依赖它。

## 环境约束（来自全局 CLAUDE.md）

- Windows + bash，路径用正斜杠或双引号；UTF-8。
- 代码无 emoji。
- 简化主流程，**不写 fallback/降级**（与本项目"三层降级引擎管理器"的设计冲突时，按设计优先，但新加代码不要自己再加 fallback）。
- 不生成测试脚本和项目文档（除非明确要求）。
- 中文 md，英文 Angular 风格 commit：`feat(scope): summary`，正文三段 `Agent-Task` / `Agent-Decision` / `Agent-Limitation`，不带 `Co-Authored-By`。
- 报错信息追加到全局 `C:\Users\baizhicong\.claude\CLAUDE.md`。
