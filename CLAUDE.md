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

## 修改记录

### 2026-06-25 修复 JSON 标签与显示问题
- `internal/models/models.go`：为所有模型结构体添加 `json` 标签，修复 gin 序列化后字段名大写导致前端无法读取的问题。

### 2026-06-25 修复 rod 引擎与多轮对话
- `internal/engine/rod_engine.go`：
  - 新增 `pages map[string]*rod.Page` 实现 page 复用，同一站点多轮对话不再新建 tab
  - `typePrompt` 支持三层输入降级：`Input()` -> JS 设置 value -> JS 设置 innerText/contentEditable
  - `submitPrompt` 支持两层提交降级：`Click()` -> JS 触发 Enter 键盘事件
  - `getLastAnswerText` 取最后一个 answer 元素（最新消息）
  - 通过 answer 元素数量变化检测新回答出现
  - 新增 `ResetPages()` 用于新会话时重置所有 page
- `internal/engine/manager.go`：新增 `PageResetter` 接口和 `ResetPages()` 方法
- `internal/api/chat.go`：
  - `ChatRequest` 新增可选 `session_id` 字段
  - 提供 `session_id` 时复用现有会话（多轮对话）
  - 仅新会话时调用 `ResetPages()`

### 2026-06-25 修复 YAML 结构与 Chrome 路径
- `configs/sites.yaml`：修复 YAML 结构，匹配 Go 结构体。原 YAML 使用 `engine_type` 和顶层 `selectors`，但 `SiteConfig` 期望 `engine.primary` 和 `engine.selectors`，导致解析出空值。同时添加 `enabled: true`。
- `internal/engine/rod_engine.go`：
  - 新增 `findChromeBinary()` 函数，检测系统 Chrome/Edge 路径，避免 rod 下载 Chromium
  - 设置 `UserDataDir("./.browser-data")` 避免沙箱写入限制
  - `typePrompt` JS 降级使用 React 兼容的 `nativeSetter` 设置 value
  - 全流程 `log.Printf` 日志：page 创建/复用、元素查找、输入、提交、轮询
- 选择器通过浏览器实际检查 DOM 获取：
  - Kimi: `div.chat-input-editor`（contenteditable）+ `div.send-button-container`
  - DeepSeek: 保留合理猜测（需登录后验证）
- **注意**：当时错误地将 Qwen URL 从 `www.qianwen.com` 改为 `chat.qwen.ai`，已在后续修复中还原

### 2026-06-25 修复 nil slice 导致前端崩溃
- `internal/storage/session_store.go`：`GetSessions` 返回值初始化为 `[]models.Session{}` 而非 `nil`，避免 JSON 序列化为 `null`
- `internal/storage/site_store.go`：`GetSites` 同理
- `internal/storage/message_store.go`：`GetMessagesBySession` 同理
- `web/src/hooks/useSites.ts`：`fetchSites` 回调加 `data || []` 防御
- `web/src/components/HistoryPanel.tsx`：`fetchSessions` 回调加 `data || []` 防御
- 根因：Go 的 `nil` slice 序列化为 JSON `null`，前端 `data.length` 报 `TypeError: Cannot read properties of null`，React 崩溃后页面空白

### 2026-06-25 前端多轮对话与历史记录常驻
- `web/src/types/index.ts`：`Message` 新增 `turn?: number`，新增 `Turn` 接口
- `web/src/App.tsx`：
  - 引入 `turn` 概念，使用 `currentTurnRef` 追踪轮次
  - 同一 session 内发送消息追加 turn，不再替换 messages
  - `HistoryPanel` 从条件渲染改为常驻左侧第一列
  - 对话区域按 turn 分组显示：用户提问横幅 + 对应回答卡片网格
  - `New Chat` 重置 turn 和 messages
- `web/src/components/HistoryPanel.tsx`：移除条件渲染相关代码，作为独立常驻侧边栏工作

### 2026-06-26 修复历史记录加载与 New Chat 实现方式

根因：(1) `handleSelectSession` 加载消息后 `turns` 设为空数组，渲染循环不显示任何内容；(2) `ResetPages()` 关闭所有标签页再重建，每次 New Chat 都重启 Chrome。

- `internal/storage/db.go`：messages 表添加 `turn INTEGER DEFAULT 0` 和 `prompt TEXT DEFAULT ''` 列（ALTER TABLE 迁移）
- `internal/models/models.go`：`Message` 添加 `Turn int` 和 `Prompt string` 字段
- `internal/storage/message_store.go`：`CreateMessage` 签名新增 `turn int, prompt string` 参数；`GetMessagesBySession` / scan 包含 `turn` 和 `prompt` 列
- `internal/storage/session_store.go`：移除重复的 `CreateMessage` 定义
- `internal/api/chat.go`：`ChatRequest` 新增 `Turn int` 字段；`CreateMessage` 调用传入 `req.Turn` 和 `req.Prompt`；新会话时调用 `StartNewChat` 替代 `ResetPages`
- `internal/engine/rod_engine.go`：
  - `Selectors` 新增 `NewChat string` 字段
  - 新增 `StartNewChat(site)` 方法：先尝试配置的 `new_chat` 选择器，失败后用 JS 文本搜索找"新建对话"/"New Chat"等按钮并点击。找不到则跳过（非致命）
- `internal/engine/manager.go`：新增 `NewChatStarter` 接口和 `Manager.StartNewChat(sites)` 方法
- `web/src/App.tsx`：
  - `handleSend` 请求体新增 `turn` 字段
  - `handleSelectSession` 从 API 返回的 `turn`/`prompt` 字段重建 `turns` 数组
  - 新会话创建后递增 `historyRefresh` 触发 HistoryPanel 刷新
- `web/src/components/HistoryPanel.tsx`：新增 `refreshTrigger` prop，变化时重新拉取会话列表

### 2026-06-26 修复多轮对话取回答错误与 DeepSeek 思考内容混入

根因：`getAnswerStatus` 取 `els[els.length-1]`（最后一个匹配元素）的文本，在多轮对话和思考模式下存在三个问题：(1) Kimi 多轮对话时不新增 `[class*=markdown]` 元素而是更新同一元素内容，`same count` 分支中 `lastText` 初始为空导致旧回答被误判为稳定；(2) DeepSeek 的 `[class*=ds-markdown]` 同时匹配思考过程容器和正式回答容器，取最长文本取到了思考过程；(3) DeepSeek 正式回答内部有多个嵌套 `ds-markdown-paragraph` 子元素，取最后一个只取到一小段。

- `internal/engine/rod_engine.go`：
  - **`getAnswerStatus` 新增 `beforeCount` 参数**：在 `beforeCount` 之后的新增元素中取文本最长的，避免取到旧回答。若新增元素均无文本则 fallback 到全局最长
  - **`same count` 分支排除旧文本**：记录 `beforeText`（发送前的回答文本），仅当 `currentText != beforeText` 时才进入稳定判定，防止 Kimi 多轮对话时旧回答被误判为新回答
- `configs/sites.yaml`：
  - DeepSeek answer 选择器从 `[class*=ds-markdown]` 改为 `.ds-assistant-message-main-content`，精确匹配正式回答容器，排除思考过程

### 2026-06-26 修复三站点后台标签页渲染与输入问题

根因：go-rod 在多标签页并发环境下，后台标签页的 React 懒渲染组件不触发 `IntersectionObserver`/`ResizeObserver` 回调，导致 Qwen 的 `qk-markdown` 内容为空；CDP `el.Input()` 命令间歇性挂起，残留 goroutine 持续发送键盘事件干扰页面。

- `internal/engine/rod_engine.go`：
  - **可见性覆盖**：通过 `proto.PageAddScriptToEvaluateOnNewDocument` 在页面导航前注入 JS，覆盖 `document.hidden`/`visibilityState`/`hasFocus`；页面加载后再次 `page.Eval` 重新注入并分发 `visibilitychange` 事件触发 React 重渲染
  - **IntersectionObserver mock**：完全替换为自定义实现，`observe()` 时立即用 `isIntersecting: true` 触发回调，不依赖浏览器后台标签页的异步回调
  - **ResizeObserver mock**：同上，`observe()` 时立即触发回调，解决 Qwen 间歇性 `answer-receiving-card` 内容不渲染
  - **移除所有 `el.Input()` 调用**：plain input 直接用 JS `document.execCommand('insertText')` 输入，失败时回退到 `nativeSetter` + `_valueTracker.setValue('')` + `input`/`change` 事件。消除 CDP 命令挂起导致的 goroutine 泄漏
  - **`submitPrompt` Enter 键回退**：按 Enter 前先通过 JS 聚焦输入元素（submit 选择器不匹配时回退到 `textarea, input, [contenteditable=true]`）
  - **pre-submit/post-submit 诊断**：对 `TEXTAREA`/`INPUT` 元素使用 `.value` 而非 `.textContent` 检查文本内容
  - **`typePrompt` 编辑器类型检测**：用单次 `page.Eval` JS 检测 Slate/Lexical/plain，替代多次 `el.Attribute()` CDP 调用（后者在多标签页环境下会挂起）。除 `data-slate-editor`/`data-lexical-editor` 属性外，还检查 `el.__lexicalEditor` 属性和 React fiber 树中的 editor 实例，解决框架初始化延迟导致误检测为 plain 的问题
  - JS 输入返回实际值用于验证，日志中打印 `value=` 确认输入成功
  - **轮询阶段 `visibilitychange` 重分发**：当 answer 元素已出现但文本为空（`currentCount > beforeCount && currentText == ""`）且轮询超过 5 次时，通过 `page.Eval` 分发 `visibilitychange`/`focus`/`pageshow` 事件触发 React 重渲染，解决 Qwen `answer-receiving-card` 偶发不渲染的遗留问题。仅触发一次（`visibilityRedispatched` 标志位）

### 2026-06-25 恢复 Qwen 官网网址并适配 Slate.js 编辑器
- `configs/sites.yaml`：
  - Qwen URL 从错误的 `https://chat.qwen.ai/` 恢复为 `https://www.qianwen.com/`
  - Qwen 选择器通过浏览器实际检查 DOM 获取：
    - input: `[contenteditable=true]`（Slate.js 编辑器）
    - submit: `button[aria-label='发送消息']`
    - answer: `[class*=answer-common-card]`
    - wait_for: `[contenteditable=true]`
- `internal/engine/rod_engine.go`：
  - 新增 `typePromptSlate()` 方法，通过 React fiber 树访问 Slate.js 编辑器实例
  - 调用 `editor.insertText(prompt)` + `onChange(editor.children)` 触发状态更新
  - `typePrompt` 检测 `data-slate-editor="true"` 属性，自动路由到 Slate.js 输入路径
  - 根因：Slate.js 维护独立内部状态，`innerText`/`execCommand('insertText')`/CDP `InputInsertText` 均无法触发发送按钮启用

### 2026-06-26 修复 New Chat 并发、DeepSeek 提交与 Qwen 后台渲染

根因：(1) `StartNewChat` 串行执行三站点，阻塞 ~20s 才开始 `SendMessage`；(2) DeepSeek 的 `div[class*=send]` 选择器不匹配实际发送按钮；(3) Qwen 后台标签页 `answer-common-card` 内容为空，缺乏 `requestAnimationFrame`/`requestIdleCallback` 导致 React 流式渲染不触发。

- `internal/engine/manager.go`：
  - **`StartNewChat` 改为并发执行**：使用 `goroutine` + `WaitGroup` 并发处理三站点，`wg.Wait()` 确保全部完成后再返回 `SendMessage`，总耗时从 ~20s 降至 ~8s
- `internal/engine/rod_engine.go`：
  - **新增 `reinjectMocks` 方法**：轻量级重新注入 IO/RO/RAF/ric mock，不等待 `WaitLoad`（适用于 SPA 按钮点击后的状态更新）
  - **`refreshPageAfterNavigation` 重构**：导航后使用 `WaitLoad` + `WaitIdle(5s)` + `reinjectMocks`；SPA 按钮点击后仅用 `reinjectMocks` + `time.Sleep(2s)`
  - **添加 `requestAnimationFrame`/`requestIdleCallback` 模拟**：用 `setTimeout(fn, 16)` 实现，避免 `queueMicrotask` 导致的 React 无限循环。Qwen 后台渲染延迟从 121s 降至 15s
  - **plain input 始终使用 nativeSetter**：移除 `execCommand('insertText')` 路径，始终用 `_valueTracker.setValue('')` 重置 + `nativeSetter.set.call(el, prompt)` + `input`/`change` 事件分发，确保 React 受控组件状态正确更新
  - **`submitPrompt` 添加通用提交按钮搜索**：配置选择器失败后，依次尝试 `button[class*=send]`/`[aria-label*=send]` 等常见模式；"near-input" 回退搜索排除 toggle/settings/menu/upload/file 按钮，按 X 坐标降序选最右侧候选（发送按钮通常在最右），解决 DeepSeek `ds-button__icon--last-child` 发送按钮找不到的问题
  - **JS Enter 键分发**：CDP `Keyboard.Press` 前先通过 JS 分发 `keydown`/`keypress`/`keyup` 事件，提高 React 键盘事件兼容性
  - **渲染重试增强**：轮询阶段重试次数从 1 次增加到 3 次，每次 `reinjectMocks` + `scrollIntoView` + `click`，加 2s 等待
  - **DeepSeek 新建对话搜索扩展**：添加 `新建会话`/`新聊天`/`创建新对话` 等关键词，`new-dialog`/`new-talk`/`chat-new`/`start-new`/`create-new`/`add-chat`/`newsession` 等 class 模式，`href` 匹配根路径或 `/chat`，未找到时输出候选元素诊断日志
- `internal/storage/db.go`：
  - **SQLite 并发写入修复**：启用 `PRAGMA journal_mode = WAL` 和 `PRAGMA busy_timeout = 5000`，解决多 goroutine 并发写入时 `database is locked (SQLITE_BUSY)` 错误

### 2026-06-26 修复 StartNewChat 后内容获取与提交问题

根因：(1) StartNewChat 清除旧 answer 元素后 beforeCount 未重置，轮询循环不处理 count 减少的情况；(2) Kimi "新建对话"按钮在屏幕外（x=-120），CDP 点击失败；(3) Qwen 提交按钮 CDP 点击后未触发 React 提交（编辑器文本未清空）；(4) getAnswerStatus 返回换行符等加载指示文本被误判为稳定回答；(5) StartNewChat 后 SPA 按钮未渲染导致搜索失败。

- `internal/engine/rod_engine.go`：
  - **轮询循环 count 减少处理**：新增 `if currentCount < beforeCount` 分支，重置 beforeCount/beforeText/lastText/stableRounds，解决 StartNewChat 清除旧 answer 元素后轮询死循环
  - **StartNewChat 完整重写**：2s 等待 SPA 渲染；优先级排序搜索（BUTTON>A>div，文本匹配>class 匹配）；CDP 鼠标点击（可见元素）+ JS click 回退（屏幕外元素）；3s 等待后验证 answer count 减少；JS click 重试 + 导航回退
  - **submitPrompt 2s 重试**：配置选择器未找到时等待 2s 重新查找，解决 StartNewChat 后提交按钮未渲染
  - **post-submit JS click 回退**：CDP 点击后编辑器仍有 prompt 文本时，先用 JS `btn.click()` 点击提交按钮，失败再回退 Enter 键
  - **getAnswerStatus 文本 trim**：`els[i].innerText || els[i].textContent || ''` 改为 `.trim()`，防止换行符等加载指示被误判为回答
  - **requiredStable 从 3 增至 5**：需 2.5s 稳定才判定为回答完成
  - **answer 日志增加文本内容**：`text=%q` 打印前 100 字符，便于诊断 1-char 回答问题
  - **typePrompt contenteditable 重试**：检测为 plain 但元素是 contenteditable 时，等待 2s 重新检测框架类型，解决 StartNewChat 后 Slate.js 编辑器初始化延迟

### 2026-06-26 修复 HTML 转 Markdown、前端渲染与格式约束提示词

根因：(1) `getAnswerStatus` 使用 `innerText` 提取文本，丢失所有 Markdown 格式（表格变成制表符分隔、代码块丢失 ``` 标记、标题丢失 # 前缀）；(2) 前端 `MessageCard` 虽有 ReactMarkdown 但传入的是纯文本；(3) `format_prompt` 字段在 `Site` model 中存在但 `chat.go` 从未使用，`sites.yaml` 中为空。

- `internal/engine/rod_engine.go`：
  - **`getAnswerStatus` 新增 `htmlToMd` JS 函数**：将渲染后的 HTML 反向转换为 Markdown 源码，支持标题(h1-h6)、段落、列表(ul/ol)、代码块(pre/code + language-xxx)、表格(thead/tbody/tr/th/td)、引用(blockquote)、链接(a)、图片(img)、粗体(strong/b)、斜体(em/i)、删除线(del/s)、分隔线(hr)
  - **UI 元素过滤**：跳过 button/svg/path 元素，跳过 class 含 `copy`/`download`/`clipboard`/`toolbar`/`action`/`code-header`/`table-cap`/`table-label`/`lang-label`/`code-lang`/`code-action`/`header-row` 的元素
  - **UI 标签文本过滤**：叶子元素（无块级子元素）且文本 < 20 字符时，与 UI 标签列表匹配（copy/download/table/python/javascript/java/go/rust/typescript/sql/html/css/bash/shell/json/yaml/xml/markdown/code/代码 等 40+ 标签），匹配则跳过
  - **正则后处理**：使用 multiline 正则 `/^\s*(Table|Python|JavaScript|...)\s*$/gim` 移除独立成行的 UI 标签，作为元素级过滤的双重保障
  - **`span` 内联处理**：`span` 归入内联元素（不加 `\n`），`div`/`section`/`article` 等归入块级元素（加 `\n`），修复表格单元格内代码跨行问题
  - **内容截断上限**：从 10000 字符提升至 50000 字符，超时从 5s 提升至 10s
  - **轮询稳定阈值提升**：`requiredStable` 从 5 增至 8（4s 稳定），新增 `minPollsBeforeStable = 20`（至少 10s 轮询后才允许判定稳定），解决 Kimi 生成表格时暂停 2.5s 被误判为回答完成导致内容截断
- `internal/api/chat.go`：
  - **`format_prompt` 自动追加**：`SendMessage` 前检查 `site.FormatPrompt`，非空时追加到用户 prompt 末尾（`prompt + "\n\n" + format_prompt`）
- `configs/sites.yaml`：
  - 三站点（qwen/kimi/deepseek）均添加默认 `format_prompt: "请使用标准Markdown格式回答，标题从第三层级（###）开始，适当使用表格、代码块、列表等结构化元素。"`
- `web/src/components/MessageCard.tsx`：
  - **完整重写**：使用 `@tailwindcss/typography` 的 `prose prose-invert prose-sm` 基础样式，配合 ReactMarkdown `components` 自定义每个元素的样式
  - **代码块渲染修复**：`code` 组件检测 `language-xxx` 类时仅渲染 `<code>` 元素（不包裹 `<pre>`），由 `pre` 组件统一处理外层包裹，避免嵌套 `<pre>` 标签
  - **表格渲染**：`table` 组件包裹 `overflow-auto rounded-md border` 容器，`th`/`td` 有明确边框和内边距
  - **滚动区域**：`max-h-[70vh] overflow-auto` 支持长内容滚动
- `web/src/components/ChatGrid.tsx`：
  - **响应式布局**：1 列单卡片 / 2 列双卡片 / 3 列三卡片，根据站点数量自动调整
- `web/tailwind.config.js`：
  - 添加 `@tailwindcss/typography` 插件
- `web/package.json`：
  - 新增 `@tailwindcss/typography` 依赖

### 2026-06-26 修复代码块语法高亮、Export 导出与单元测试

根因：(1) 前端代码块使用纯 `<pre><code>` 渲染，无语法高亮库，行号与代码内容粘连；(2) `ExportPanel` 默认 `filterKept=true`，当无消息标记为 kept 时导出只剩 session 标题；(3) Markdown 导出无 `Content-Disposition` 头，浏览器在新标签页打开文本而非下载；(4) `countHeadingLevel` 函数未限制最大层级，7 个 `#` 仍返回 7（Markdown 规范只支持 H1-H6）；(5) 项目无任何测试，功能改进缺乏系统性验证。

- `web/src/components/MessageCard.tsx`：
  - **集成 `react-syntax-highlighter`**：`pre` 组件提取代码内容和语言标识后，使用 `SyntaxHighlighter`（Prism + oneDark 主题）渲染，支持 Python/JavaScript/Go/Java/Rust/SQL/HTML/CSS/Bash/JSON/YAML 等 270+ 语言自动高亮
  - **行号分离渲染**：`showLineNumbers` + `lineNumberStyle`（右对齐、`paddingRight: 1.5em`、`minWidth: 2.5em`、`userSelect: none`），行号与代码内容有明确间距
  - **代码字体**：`Cascadia Code`/`Fira Code`/`JetBrains Mono`/`Consolas` 等等宽字体
  - **`code` 组件简化**：有 `language-xxx` 类时透传给 `pre` 处理，无类名时渲染为内联代码（粉色背景）
- `web/src/components/ExportPanel.tsx`：
  - **`filterKept` 默认值从 `true` 改为 `false`**：默认导出所有消息，而非仅导出已标记 kept 的消息
  - 标签文本从"仅 keep"改为"仅导出已保留"
- `internal/api/export.go`：
  - **Markdown 导出添加 `Content-Disposition` 下载头**：`attachment; filename="export_YYYYMMDD_HHMMSS.md"`，浏览器触发下载而非打开文本
  - 添加 `Content-Type: text/markdown; charset=utf-8`
- `internal/export/markdown_exporter.go`：
  - **`countHeadingLevel` 修复**：添加 `count <= 6` 条件，超过 6 个 `#` 返回 0（符合 Markdown 规范 H1-H6）
- `internal/export/export_test.go`（新增）：
  - 13 个测试用例覆盖 `ToMarkdown`、`ToJSON`、`offsetHeadings`、`countHeadingLevel`：含消息/空消息/未知站点/多轮对话/标题偏移/代码块内标题保护/JSON 往返一致性
- `internal/storage/storage_test.go`（新增）：
  - 10 个测试用例覆盖 `CreateSession`/`GetSessionByID`/`GetSessions`/`CreateMessage`/`GetMessagesBySession`/`UpdateMessageKept`/`filter_kept` 行为/多轮排序/站点 CRUD，使用 `:memory:` SQLite
- `web/package.json`：
  - 新增 `react-syntax-highlighter` 和 `@types/react-syntax-highlighter` 依赖

### 2026-06-26 重构内容提取架构，修复代码块格式问题

根因：(1) Qwen 的 html2md 提取将渲染行号混入代码内容（`1def` 而非 `def`）；(2) DeepSeek 和 Qwen 的代码块缺少 `language-xxx` 类，导致前端语法高亮无法识别语言；(3) 内容提取逻辑散落在 `rod_engine.go` 中，新增站点时难以维护。

- `internal/engine/content_extractor.go`（新增）：
  - **`ContentExtractor` 接口**：`Extract(page, answerSelector, beforeCount, expectedLength) (string, error)`，统一内容提取抽象
  - **`ClipboardExtractor`**：覆盖 `navigator.clipboard.writeText`/`document.execCommand('copy')`/`DataTransfer.setData`/`copy` 事件，通过评分系统查找答案级复制按钮（文本"复制"/"copy"=100，aria-label=80，class"copy"=60，action 容器内 SVG=10-50），`isCodeBlockButton` 过滤代码块复制按钮（`closest('pre')`/class 模式/兄弟 `<pre>` 检查），所有候选合并尝试返回最长文本，基于 `expectedLength` 的动态提前退出阈值 `Math.max(500, expectedLength*0.5)`
  - **`HtmlToMarkdownExtractor`**：HTML 转 Markdown，`pre` case 增强：克隆代码元素并移除 `[class*="line-number"]`/`[data-line-number]` 等行号元素；检测前 5 行以 1/2/3/4/5 开头时正则移除行号 `^\s*\d+(?![0-9])`；语言检测四层降级（class `language-xxx` → `data-language` 属性 → 前兄弟元素文本匹配 → 代码内容启发式检测 `def`→python, `func`→go, `fn`→rust, `#include`→cpp 等）
  - **`HybridExtractor`**：先尝试 ClipboardExtractor，失败或文本长度 < `expectedLength/2` 时回退到 HtmlToMarkdownExtractor
  - **`NewContentExtractor(strategy, copyButtonSelector)`**：工厂函数，`strategy="clipboard"` 返回 HybridExtractor，否则返回 HtmlToMarkdownExtractor
- `internal/engine/rod_engine.go`：
  - `Selectors` 新增 `CopyButton` 和 `ContentStrategy` 字段
  - `getAnswerStatus` 轮询阶段使用轻量 `innerText` 提取，`done:` 标签处调用 `extractor.Extract(page, sels.Answer, beforeCount, len(lastText))`，提取失败回退到轮询文本
  - `StartNewChat` 成功路径增加 `page.WaitIdle(5s)` 等待页面加载完成再重新注入 mocks，修复 Kimi 新建对话后答案不渲染导致 120s 超时
- `configs/sites.yaml`：三站点均添加 `copy_button: ""` 和 `content_strategy: "clipboard"`

### 2026-06-29 修复 New Site 保存与历史记录管理

根因：(1) 前端 `SiteConfigModal` 的 `selectors` 字段是 JSON 字符串（textarea 内容），但后端 `CreateSiteRequest.Selectors` 期望 `map[string]string`，`ShouldBindJSON` 反序列化失败返回 400，前端仅 `console.error` 无用户可见反馈；(2) 后端 `handleCreateSite`/`handleUpdateSite` 硬性要求 `input`/`submit`/`answer` 选择器非空，新站点无法保存；(3) `openEditSite` 未加载已有 selectors（设为空字符串），且 `Site` 类型缺少 `selectors` 字段；(4) `SiteSidebar` 对 `enabled=false` 的站点禁用编辑按钮，导致未配置站点无法点击编辑；(5) 历史记录面板无收起/删除功能，后端无 `DELETE /api/sessions/:id` 端点。

- `internal/api/sites.go`：
  - **`handleCreateSite` 选择器改为可选**：移除 `input`/`submit`/`answer` 非空校验，改为 `enabled := req.Selectors["input"] != "" && req.Selectors["submit"] != "" && req.Selectors["answer"] != ""`，无选择器时站点保存为 `enabled=false`
  - **`handleUpdateSite` 选择器改为可选**：移除非空校验，根据选择器完整性动态设置 `enabled`
- `internal/storage/session_store.go`：
  - **新增 `DeleteSession`**：事务内先删 `messages` 再删 `sessions`，`RowsAffected==0` 返回 `session not found`
- `internal/api/chat.go`：
  - **新增 `handleDeleteSession`**：`DELETE /api/sessions/:id`，调用 `storage.DeleteSession`
- `internal/api/server.go`：注册 `DELETE /api/sessions/:id` 路由
- `web/src/types/index.ts`：`Site` 接口新增 `selectors: string` 字段
- `web/src/App.tsx`：
  - **`openEditSite` 加载已有 selectors**：`selectors: site.selectors || ''` 替代空字符串
  - **`handleSaveSite` 解析 selectors JSON**：`JSON.parse(formData.selectors)` 转为对象后发送，解析失败 `alert` 提示且不关闭弹窗；HTTP 错误 `alert` 展示后端错误信息
  - **新增 `handleDeleteSession`**：调用 `DELETE /api/sessions/:id`，删除当前会话时触发 `handleNewChat`，刷新历史列表
  - **新增 `historyCollapsed` 状态**：控制 HistoryPanel 收起/展开
- `web/src/components/SiteConfigModal.tsx`：
  - **`createEmptyForm` 提供默认 selectors 模板**：包含 `input`/`submit`/`answer`/`wait_for`/`copy_button`/`content_strategy` 六个字段的结构化 JSON
- `web/src/components/SiteSidebar.tsx`：
  - **编辑按钮始终可点击**：移除 `disabled={disabled}`，未配置站点可点击编辑添加选择器
- `web/src/components/HistoryPanel.tsx`：
  - **收起/展开功能**：`collapsed` 为 true 时渲染 40px 窄条 + 右箭头展开按钮；展开时标题栏显示左箭头收起按钮
  - **单条删除按钮**：每条记录右侧显示垃圾桶图标（`opacity-40` 常驻可见，hover 高亮），点击触发 `window.confirm` 确认后调用 `onDeleteSession`

### 2026-06-29 修复登录态复用、WebSocket 崩溃与表单简化

根因：(1) `NewRodEngine` 使用 `UserDataDir("./.browser-data")` 独立配置文件，无法访问用户 Chrome 的登录态；Chrome 的远程调试不允许使用默认配置目录，直接用 `--user-data-dir` 指向真实配置会报 `DevTools remote debugging requires a non-default data directory`；(2) `Hub.Broadcast` 用 `RLock` 并发读取连接列表后，多个 goroutine 同时对同一连接调用 `WriteJSON`，gorilla/websocket 不支持并发写入 → `panic: concurrent write to websocket connection`，服务器崩溃；(3) 新增站点表单显示 6 个字段（ID/名称/链接/引擎类型/Selectors JSON/Format Prompt），用户体验差；(4) `handleSubmit` 中 `handleDetect` 通过 `setFormData` 异步更新 selectors，但 `onSave(formData)` 立即使用了旧状态，导致保存的 selectors 为空。

- `internal/engine/rod_engine.go`：
  - **新增 `getChromeUserDataDir()`**：检测 `C:\Users\<user>\AppData\Local\Google\Chrome\User Data` 和 Edge 对应路径
  - **新增 `syncChromeProfile(targetDir)`**：首次运行时（`./.browser-data/Local State` 不存在）从用户 Chrome 配置复制 `Local State`（含加密密钥）、`Default/Network/Cookies`（或 `Default/Cookies`）、`Default/Login Data` 到 `./.browser-data`，复用用户 Chrome 的登录态
  - **新增 `copyFile`/`isChromeRunning` 辅助函数**
  - `NewRodEngine` 改为：先检查 `./.browser-data` 是否已有 `Local State`，没有则触发 `syncChromeProfile`，然后用 `./.browser-data` 启动
  - 新增 `os/exec` 和 `path/filepath` import（用于 tasklist 检测和路径拼接）
- `internal/api/websocket.go`：
  - **引入 `clientEntry` 结构**：每个连接关联一个 `sync.Mutex`，`WriteJSON` 前加锁，解决并发写入 panic
  - `Hub.Broadcast` 改为两阶段：先 `RLock` 收集所有 `clientEntry` 快照，再逐个加锁写入，失败的连接在第二阶段统一清理
  - `Hub.Add`/`Hub.Remove` 改为操作 `map[*websocket.Conn]*clientEntry`
- `web/src/components/SiteConfigModal.tsx`：
  - **表单简化为 2 个字段**：默认只显示「名称」和「链接」，ID/引擎类型/Selectors/Format Prompt 收纳到「▶ 高级设置」折叠区
  - **ID 自动生成**：输入名称时 `slugify(name)` 自动填充 ID（中文保留，非字母数字转为 `-`）
  - **保存时自动检测**：`handleSubmit` 中 `!isEditing && !autoDetected` 时先调用 `handleDetect()`，成功后用返回的 selectors 字符串直接传给 `onSave`（不依赖 `setFormData` 异步更新），解决 selectors 为空的 bug
  - **`handleDetect` 返回 `string | null`**：返回检测到的 selectors JSON 字符串，调用方可直接使用
  - 默认 `format_prompt` 预填标准 Markdown 格式约束

## 开发与验证约定

### 2026-06-29 重构 Auto Detect 检测器与历史记录批量管理

根因：(1) `detector.go` 用 `launcher.New()` 尝试下载 Chromium 而非系统 Chrome，与主引擎环境不一致；(2) 检测器候选选择器太少（3 个 submit、4 个 answer），对现代 SPA 站点不够；(3) `gson.JSON.Str()` 对 null 值返回字面量字符串 `"<nil>"` 而非空字符串，导致 null 值未被过滤，API 返回 `{"answer":"<nil>"}` 这样的脏数据；(4) `enabled` 逻辑要求 input+submit+answer 三者齐全，但 rod 引擎对 submit（近邻按钮搜索 + Enter 回退）和 answer（新元素检测）均有降级，只需 input 即可工作；(5) 历史记录逐条删除按钮繁琐，用户体验差。

- `internal/engine/detector.go`：
  - **使用系统 Chrome**：调用 `findChromeBinary()` 复用 rod 引擎的 Chrome 路径，避免下载 Chromium
  - **扩展候选选择器**：input 14 个候选、submit 17 个候选、answer 16 个候选、copy_button 5 个候选
  - **近邻按钮搜索**：submit 未匹配时，从 input 元素父级向上遍历 4 层，查找 `button/[role=button]/div[class*=icon]/div[class*=btn]`，排除 toggle/settings/menu/upload/file 按钮，按 X 坐标降序选最右侧（发送按钮通常在最右）
  - **过滤 `<nil>` 字符串**：`gson.JSON.Str()` 对 null 返回 `"<nil>"`，增加 `s != "<nil>"` 过滤
  - **wait_for 自动推导**：answer 非空时自动设为 `answer + ":last-child"`
- `internal/api/sites.go`：
  - **enabled 逻辑放宽**：`handleCreateSite` 和 `handleUpdateSite` 改为 `enabled = req.Selectors["input"] != ""`，只需 input 选择器即可启用站点
- `web/src/components/SiteConfigModal.tsx`：
  - **默认 format_prompt**：新建站点时预填 `请使用标准Markdown格式回答，标题从第三层级（###）开始...`，与其他站点一致
- `web/src/components/HistoryPanel.tsx`：
  - **移除逐条删除按钮**：不再每条记录显示垃圾桶图标
  - **管理按钮 + 批量选择模式**：标题栏新增"管理"按钮，点击后进入批量模式：每条记录显示复选框，底部工具栏显示"全选"/"已选 N"/"删除"/"取消"，选择后点击删除批量删除
  - **退出管理模式**：点击"取消"或删除完成后自动退出，清空选中状态
- `web/src/App.tsx`：
  - **`handleDeleteSession` 改为 `handleDeleteSessions`**：接收 `string[]` 批量删除，循环调用 DELETE API，删除当前会话时触发 `handleNewChat`

### 2026-06-29 修复 context canceled 错误与 UI 布局问题

根因：(1) `syncChromeProfile` 复制用户 Chrome 的 `Local State` 文件到 `.browser-data`，该文件包含指向用户 Chrome 配置目录的 profile 引用，在独立 `.browser-data` 中不存在对应 profile，导致 Chrome 启动后 CDP 连接不稳定/崩溃，所有 `page.Eval` 调用返回 `context canceled`；(2) `.browser-data` 目录中残留的 `lockfile`/`SingletonLock` 等锁文件阻止 Chrome 正常启动；(3) ChatGrid 使用固定 `grid-cols-3` 无法根据卡片数量动态调整列数；(4) InputArea 使用 `<input type="text">` 只支持单行输入；(5) App header / HistoryPanel header / SiteSidebar header 使用 `py-3` 内边距导致高度不一致。

- `internal/engine/rod_engine.go`：
  - **移除 `syncChromeProfile` 及相关函数**：删除 `syncChromeProfile`、`getChromeUserDataDir`、`isChromeRunning`、`copyFile`，以及 `io`/`os/exec`/`path/filepath` 导入
  - **新增 `cleanupLockFiles`**：启动前删除 `.browser-data` 目录中的 `lockfile`/`SingletonLock`/`SingletonSocket`/`SingletonCookie`/`DevToolsActivePort`，防止残留锁文件阻止 Chrome 启动
  - **新增 `ensureBrowser()` 方法**：`SendMessage` 开头调用，通过 `browser.Pages()` 检测浏览器连接状态，连接断开时自动清理锁文件并重新启动 Chrome；`browser == nil`（初始启动失败）时也尝试重新启动
  - **`RodEngine` 结构体新增字段**：`controlURL string` 和 `userDataDir string`，用于重连时记录 Chrome 调试 URL 和用户数据目录
  - **`NewRodEngine` 简化**：启动前调用 `cleanupLockFiles`，失败时返回 `browser=nil` 的引擎实例（由 `ensureBrowser` 延迟重试）
  - **选择器检查放宽**：`SendMessage` 中从 `sels.Input == "" || sels.Answer == ""` 改为仅 `sels.Input == ""`
- `web/src/components/InputArea.tsx`：
  - **从 `<input>` 改为 `<textarea>`**：`rows={2}`，`resize-none`，`minHeight: 44px`，`maxHeight: 200px`
  - **自动调整高度**：`useEffect` 监听 `prompt` 变化，`scrollHeight` 驱动高度（上限 200px）
  - **Enter 发送 / Shift+Enter 换行**：`handleKeyDown` 拦截 Enter 键
- `web/src/components/ChatGrid.tsx`：
  - **动态等宽列**：从 Tailwind `grid-cols-1 lg:grid-cols-2 lg:grid-cols-3` 改为内联样式 `gridTemplateColumns: repeat(${count}, minmax(0, 1fr))`，按实际卡片数量等分宽度
- `web/src/components/SiteSidebar.tsx`：
  - **头部高度统一**：从 `px-4 py-3` 改为 `flex h-12 items-center px-4`
- `web/src/components/HistoryPanel.tsx`：
  - **头部高度统一**：从 `px-4 py-3` 改为 `flex h-12 items-center justify-between px-4`
- `web/src/App.tsx`：
  - **App header 高度统一**：从 `px-4 py-3` 改为 `flex h-12 items-center justify-between px-4`

### 2026-06-29 修复 Google 登录阻止与实现可拖拽调整面板尺寸

根因：(1) go-rod launcher 默认添加 `--enable-automation` 标志，Google 检测到 Chrome 被 CDP 控制后阻止登录（"Couldn't sign you in"）；(2) `navigator.webdriver` 为 `true` 也被 Google 检测为自动化浏览器；(3) 前端面板宽度/高度固定，用户无法调整 HistoryPanel、SiteSidebar 的列宽和 InputArea 的高度。

- `internal/engine/rod_engine.go`：
  - **新增 `createLauncher` 函数**：统一 launcher 配置，`Delete("enable-automation")` 移除自动化标志，`Delete("useAutomationExtension")` 移除扩展，`Set("disable-blink-features", "AutomationControlled")` 阻止 Blink 引擎的自动化检测
  - **`ensureBrowser` 和 `NewRodEngine` 使用 `createLauncher`**：避免重复配置
  - **注入 JS 覆盖自动化检测**：`navigator.webdriver` 设为 `undefined`，`navigator.languages` 设为 `['zh-CN', 'zh', 'en']`，`navigator.plugins` 设为非空数组，`window.chrome` 设为 `{runtime: {}}`，使 Chrome 表现为正常用户浏览器
- `internal/engine/detector.go`：
  - **同样移除自动化标志**：`Delete("enable-automation")` 和 `Set("disable-blink-features", "AutomationControlled")`
- `web/src/components/ResizeHandle.tsx`（新增）：
  - **通用拖拽调整组件**：支持 `horizontal`（列宽）和 `vertical`（行高）两个方向，mousedown 捕获初始位置，document mousemove 计算增量 delta，mouseup 释放；拖拽时设置 `document.body.style.cursor` 和 `userSelect: 'none'`；`data-resize-handle` 属性方便定位
- `web/src/components/HistoryPanel.tsx`：
  - **新增 `width` prop**：从 `w-64` 改为 `style={{ width: '${width}px' }}`
- `web/src/components/SiteSidebar.tsx`：
  - **新增 `width` prop**：从 `w-56` 改为 `style={{ width: '${width}px' }}`
- `web/src/components/InputArea.tsx`：
  - **新增 `height` prop**：form 使用 `style={{ height: '${height}px' }}`，textarea 使用 `style={{ height: '100%' }}` 填满
  - 移除自动调整高度逻辑（改由拖拽控制）
- `web/src/App.tsx`：
  - **新增三个尺寸状态**：`historyWidth`（默认 256，范围 160-500）、`sidebarWidth`（默认 224，范围 150-400）、`inputHeight`（默认 80，范围 60-400）
  - **三个 ResizeHandle**：HistoryPanel 右侧（horizontal）、SiteSidebar 右侧（horizontal）、InputArea 上方（vertical，delta 取反使向上拖增大高度）
  - HistoryPanel 收起时隐藏对应的 resize handle

### 2026-06-30 修复多站点输入、提交与回答获取问题

根因：(1) Kimi 的 Lexical 编辑器 `typePromptLexical` 首次 `execCommand('insertText')` 部分插入文本后重试会叠加，导致文本重复；(2) MiniMax 的 ProseMirror 编辑器输入成功但 `button[class*=primary]` 找到的是错误按钮，通用提交搜索需要等待发送按钮启用；(3) Doubao 的 `[class*=markdown]` 选择器匹配 0 个元素，AI 回答在 `[class*=v_list_row]` 容器中；(4) ChatGPT 的 Cloudflare Turnstile 检测到 CDP 自动化，无限循环验证（无法修复）。

- `internal/engine/rod_engine.go`：
  - **`typePromptLexical` 完全重写**：改为 `async` 函数，使用 Lexical API `root.clear()` 替代 `execCommand('delete')` 清空编辑器，操作间加 `await setTimeout(150ms)` 延迟，重试也使用 `root.clear()` 清空后重新插入
  - **通用提交搜索 2s 等待**：`generic submit button search` 前加 `time.Sleep(2s)` 等待发送按钮从 disabled 变为 enabled
  - **空 Submit 时 JS click 回退**：`sels.Submit` 为空时使用通用搜索（从 input 父级向上遍历 8 层找按钮，排除 input 相关按钮，按 X 坐标降序选最右侧）
  - **Enter/Ctrl+Enter 回退**：JS click 失败后依次尝试 Enter 和 Ctrl+Enter，均失败时输出按钮诊断日志（按距离 input 排序的 top 10 按钮）
  - **轮询阶段 fallback 选择器自动切换**：`pollCount==10` 且 `currentCount==0` 时，依次尝试 28 个 fallback 选择器，找到可见元素则切换选择器并设置 `beforeCount=fbCount`、`beforeText=fbText`（避免用户消息被误判为新回答）
  - **iframe 检测**：fallback 选择器均失败时检测可见 iframe 并输出诊断
  - **pollCount==30 父容器诊断**：当 `currentCount <= beforeCount` 时输出所有含结构化内容（ul/ol/pre/table/blockquote/h1-h6）的父容器，按文本长度排序，输出 tag/class/textLen/children/pos/size/text
  - **pollCount==30 叶子元素诊断**：同时输出所有非侧边栏/导航的叶子元素（无块级子元素），按 Y 坐标排序
  - **诊断排除侧边栏**：所有诊断 JS 均过滤 `sidebar`/`w-sidebar`/`nav`/`menu` class 及其子元素
- `configs/sites.yaml`：
  - Doubao answer 选择器从 `[class*=markdown]` 改为 `[class*=v_list_row]`

### 2026-06-30 修复 GLM 思考过程提取、Doubao 内容不全与 Chrome 重连

根因：(1) GLM 的 `[class*=markdown]` 选择器同时匹配思考过程容器和正式回答容器，`getAnswerStatus` 和内容提取器未过滤思考过程元素；(2) Doubao 提交后 `postAnswerCount=0`（元素未加载），`beforeCount` 保持为 0 导致用户消息被包含在提取内容中；(3) Doubao 的 AI 回答在已有 `v_list_row` 元素中渲染（count 不变），`getAnswerStatus` 的 fallback 逻辑返回所有元素中最长文本（可能是旧消息），无法检测到新回答；(4) pollCount==30 的 fallback 选择器切换过于激进，在 AI 尚未响应时就切换到错误选择器；(5) 手动关闭 Chrome 后 `browser.Pages()` 调用永久阻塞，导致所有后续请求失败。

- `internal/engine/rod_engine.go`：
  - **`getAnswerStatus` 新增 `isInThinking` 过滤**：3 层祖先检查 + 特定 class 模式（`think-block`/`think-content`/`think_process`/`thinking-block`/`thinking-content`/`reasoning-block`/`reasoning-content`/`thought-block`），过滤后无文本则 fallback 到原始行为
  - **`getAnswerStatus` `startIdx >= els.length` 分支**：当 `beforeCount >= 元素总数` 时，检查最后一个元素（AI 回答所在）而非所有元素中最长文本，解决 Doubao 同 count 场景下无法检测新回答的问题
  - **post-submit 诊断重试**：`postAnswerCount <= beforeCount` 时等待 3 秒重新检查，允许页面元素加载完成后再更新 `beforeCount`
  - **pollCount==30 fallback 条件收紧**：从 `currentCount <= beforeCount` 改为 `currentCount < beforeCount || (currentCount == 0 && beforeCount == 0)`，避免 AI 尚未响应时误触发选择器切换
  - **pollCount==30 fallback `beforeCount` 修复**：切换选择器时设置 `beforeCount = fbTotal`（总元素数）而非 0，并要求 `fbText > 200` 字符，避免提取到用户消息
  - **`beforeCount == 0` 时 prompt 剥离安全网**：若提取内容以用户 prompt 开头，JS 查找不含 prompt 的元素索引并重新提取
- `internal/engine/content_extractor.go`：
  - **`ClipboardExtractor` 新增 `isInThinking`**：跳过思考区域内的复制按钮，过滤 `answerEls` 排除思考元素
  - **`ClipboardExtractor` click 错误处理**：`try { el.click() } catch { dispatchEvent }` 防止非函数元素报错
  - **`HtmlToMarkdownExtractor` `isInThinking` 收紧**：从 10 层 + 宽泛模式改为 3 层 + 特定模式，避免误过滤正式回答
  - **`HtmlToMarkdownExtractor` innerText fallback UI 标签过滤**：raw innerText 使用前正则移除独立成行的 UI 标签（python/运行/copy/复制等 40+ 标签）

### 2026-07-01 修复多轮对话内容提取不对应

根因：`HtmlToMarkdownExtractor.Extract` 的 fallback 在 `beforeCount >= els.length`（多轮对话中的常见情况）时遍历所有 answer 元素并取最长文本，返回了上一轮的旧回答而非最新回答。`getAnswerStatus` 的 `else` 分支也有类似问题。次要 bug：`parts.join('\\n\\n')` 使用字面量 `\n\n` 而非实际换行符。

- `internal/engine/content_extractor.go`：
  - **`HtmlToMarkdownExtractor.Extract` fallback 改为从最后一个元素向前查找**：取第一个非空、非思考过程的元素，而非遍历所有元素取最长。匹配 `getAnswerStatus` 的 `startIdx >= els.length` 分支行为
  - **修复 join 分隔符**：`parts.join('\\n\\n')` 改为 `parts.join('\n\n')`，使用实际换行符
- `internal/engine/rod_engine.go`：
  - **`getAnswerStatus` `else` 分支改为看最后一个元素**：从遍历所有元素取最长改为取最后一个元素的文本，与 `startIdx >= els.length` 分支行为一致
  - **修复内联 `htmlToMd` 的 join 分隔符**：同上 `\\n\\n` → `\n\n`
- `export_session.py`（新增）：从 SQLite 数据库导出会话数据为 JSON，支持 `list`/`export`/`summary` 命令，用于验证多轮对话内容提取
- `check_chat_mismatch.py`（重写）：读取导出的 JSON，用 LLM 判定每条回答是否与 prompt 对应，输出对应/不对应/无法判断及原因
- `test_multi_turn.py`（新增）：通过 API 发送多轮对话，轮询等待所有站点完成，自动导出并运行 LLM 验证
- 验证结果：修复前 DeepSeek Turn 2 返回 Turn 1 的旧回答；修复后 DeepSeek 和 GLM 正确返回 "3+1=4" 的计算结果

### 2026-07-07 修复 Qwen 导航重定向与 DeepSeek 流式生成中断

根因：(1) Qwen 首页加载后通过 `window.location.href = '/record'` 直接赋值跳转，绕过了 `history.pushState`/`replaceState` 和 `location.assign`/`replace` 的拦截，导致页面导航到非聊天页面（/record、/write 等），触发新建标签和功能异常；(2) DeepSeek 答案在流式生成中途（~500-1000 chars 时）页面完全重置到欢迎页状态（`bodyTextPreview: "Start chatting with Instant..."`），答案元素消失，内容丢失。此前误判为虚拟列表回收，实际根因是 `closeOverlays` 函数在轮询期间每 5 秒定期执行，其内部包含 3 次 `Escape` 键分发和广谱的 `[class*="close"]`/`[class*="back"]`/`[class*="cancel"]` 选择器点击，在 DeepSeek 上会取消生成并重置 SPA 视图。

- `internal/engine/rod_engine.go`：
  - **`Location.prototype.href` setter override**：拦截 `window.location.href = '...'` 直接赋值（Qwen 首页的重定向方式），通过 `Object.getOwnPropertyDescriptor(Location.prototype, 'href')` 获取原生 setter，包装后检查 `isBlockedURL(url)` 再调用原生 setter
  - **扩展 `blockedPaths`**：从 Qwen 专属路径扩展为 `['/record', '/recording', '/write', '/writing', '/agent', '/canvas', '/draw', '/paint', '/workspace']`，覆盖所有非聊天功能页面
  - **`window.open` 和 click handler 加 `isBlockedURL` 检查**：阻止通过新标签或链接点击导航到非聊天页面
  - **移除轮询期间的定期 `closeOverlays` 调用**：删除 `pollCount%10 == 0` 分支中的两处 `re.closeOverlays(page)` 调用（循环开头和 `continuePolling:` 标签后），`closeOverlays` 仅在答案稳定后（`done:` 标签处）的内容提取前调用
  - **移除 `closeOverlays` 中的 `Escape` 键分发**：3 次 `keydown`/`keyup` Escape 事件在 DeepSeek 等聊天站点会取消正在进行的生成
  - **收窄 `closeOverlays` 的选择器**：从 `closeSelectors` 中移除 `[class*="close"]`/`[aria-label*="close"]`/`[class*="cancel"]`/`[class*="back"]`/`[class*="dismiss"]` 等广谱选择器，仅保留明确的面板关闭选择器（`dialog-close`/`modal-close`/`lightbox-close`/`preview-close`/`zoom-close` 等），避免误点聊天界面的功能按钮
- `configs/sites.yaml`：
  - DeepSeek `submit` 选择器从 `div[class*=send]`（返回 0 元素）改为 `div.ds-button--primary`
  - DeepSeek `answer` 选择器从 `.ds-markdown`（匹配 5 个元素含思考过程）改为 `.ds-assistant-message-main-content`（仅匹配最终答案）
- 验证结果：
  - DeepSeek Turn 1：返回 5463 字符（PowerShell 字符计数）/ 8499 字节（Go 字节计数），包含表格、标题、列表、引用块、分隔线
  - DeepSeek Turn 2（复用 session）：返回 236 字符，包含代码块（`3 + 1 = 4`）、标题、表格，`startIdx=1` 正确提取第二个答案元素
  - Qwen：URL 跳转至 `/chat/<session-id>` 而非 `/record`，无额外标签，Slate.js 输入和提交正常

## 开发与验证约定

每次修改代码后，必须按以下顺序执行编译与运行：

```bash
# 1. 编译前端
cd web && npm run build

# 2. 编译后端
cd .. && go build -o bin/server.exe cmd/server/main.go

# 3. 运行（确保旧进程已停止）
make dev
```

前端修改后必须先 `npm run build`，因为 `internal/api/static.go` 用 `//go:embed` 内嵌产物。直接 `go build` 会 embed 旧文件。
