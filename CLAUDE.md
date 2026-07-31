# CLAUDE.md

## 项目

多站点大模型官网站点聊天聚合器（Qwen / Kimi / DeepSeek 等）。一次输入并发发送到多个已启用的站点，UI 中按站点网格展示回答，可对每条回答做 keep/remove，支持按 keep 过滤导出 JSON 或 Markdown。

技术栈：Go 1.23 (gin + go-rod + playwright-go) + React 18 + Vite + SQLite。单二进制部署：Go 服务把构建后的前端静态文件 `embed` 内嵌并直接托管。

## 构建与运行

```bash
# 一键构建（前端 + 后端，产出 Windows GUI 单二进制 bin/KnowledgeClip.exe）
build.bat          # Windows 推荐
# 或
make build         # 等价

# 运行
make dev           # go run ./cmd/server，默认监听 :8080
# 生产产物：双击 bin/KnowledgeClip.exe
```

**构建顺序**：先 `npm run build`（输出到 `internal/api/static/`），再 `go build`。`internal/api/static.go` 用 `//go:embed` 内嵌该目录；`internal/api/static/assets/` 是前端构建产物、不入 git，仓库内保留自包含的占位 `index.html`，保证未构建前端时 `go build` 依然可编译（页面提示"前端尚未构建"）。

## 架构

```
cmd/server/main.go
  └─ internal/api         (Gin 路由 + WebSocket Hub)
  └─ internal/engine      (浏览器引擎管理器 + 三层降级实现)
  └─ internal/storage     (SQLite 仓库函数，按表分文件)
  └─ internal/config      (sites.yaml 加载/保存)
  └─ internal/models      (Site / Session / Message / SiteCookie)
  └─ internal/export      (JSON / Markdown 序列化)
```

### 后端 (`internal/`)

- `api/`：按职责拆文件。`chat.go` 创建 session 后立即返回 `session_id`，后台 goroutine 并发发请求，完成时 WebSocket 广播。
- `engine/`：三层降级（rod → playwright-go → ts-playwright 占位）。`rod_engine.go` 是核心实现，含 cookie 持久化、Slate/Lexical 编辑器输入、HTML 转 Markdown。
- `storage/`：仓库函数按表分文件。`Enabled`/`Kept` 在 DB 中是 INTEGER，model 中是 bool。

### 前端 (`web/src/`)

单页 React，全部状态在 `App.tsx`。主要组件：`SiteSidebar`（站点勾选）、`HistoryPanel`（历史记录）、`ChatGrid`/`MessageCard`（回答网格）、`InputArea`（发送 prompt）、`ExportPanel`（导出）、`SiteConfigModal`（站点配置）。

## 关键约定

- **站点配置双向同步**：启动时 YAML → DB，UI 增删改后 DB → YAML。改站点只走 API。
- **Selectors 是 JSON 字符串**：DB 列里是 `TEXT`，通过 `json.Unmarshal` 成结构体。
- **Cookie 持久化**：仅 `RodEngine` 实现。Playwright 引擎不持久化。
- **`/api/chat` 立即返回 session_id**：长任务在 goroutine 里跑，WebSocket 推结果。
- **端口**：后端 `:8080`；Vite dev 代理 `/api` 和 `/ws` 到 `http://localhost:8080`。

## 最近修复

### 2026-07-31 browser-act 默认无头模式（不再弹窗抢焦点）

根因：browser-act 的 `tab switch` 会调用 CDP `Target.activateTarget`，headed Chrome 借此把窗口还原并置前；引擎每次操作（发送/轮询/提取）前都要切换站点 tab，导致浏览器反复弹到桌面最前。

修复：`browser_act_engine.go` 的 `openOrReuseTab` 默认不再传 `--headed`（Chrome 以 `--headless` 后台运行，不显示窗口、不抢焦点）；设置环境变量 `BROWSER_ACT_HEADED=1` 可强制有头（登录、验证码等场景）。已实测无头模式下 Kimi/DeepSeek/GLM/豆包均正常（复用现有登录态）。

### 2026-07-31 修复 Kimi 回答内容缺失

根因：`kimi/extract_answer.js` 取页面**全局最后一个** `.markdown-container` 作为回答，而 Kimi 把代码块渲染为独立的 `markdown-container.markdown-code` 副本（全屏/teleport 用），DOM 顺序上排在主回答之后，导致只提取到代码块，正文（标题、表格、列表、结尾段）全部丢失。

修复：
- `extract_answer.js` 改为从最后一条 `.chat-content-item-assistant` 消息提取，遍历其全部非思考 `.markdown-container`（跳过 `toolcall-content-text` 思考块与 `markdown-code` 重复容器），消息内无 markdown 容器时（如限流提示）回退到消息文本。
- `wait_answer.js` 改跟踪整个最后 assistant 消息的文本稳定性（而非最后一个容器），并加消息计数守卫：仅当出现发送时不存在的新消息才判完成，避免把上一轮回答误判为新回答、或在代码块后的续文仍在流式时提前结束。
- `send_prompt.js` 记录发送时的 assistant 消息数供 wait 使用。
- `_lib.js`：uiLabels 过滤仅在不位于 `td/th` 内时生效（避免误删表格单元格内容如"**表格**"）；uiLabels 增加"预览"。

### 2026-07-31 修复 browser-act eval 代理编码崩溃

根因：`browser_act_engine.go` 中 `evalScript` 函数未设置 `PYTHONUTF8=1` 环境变量，Windows 系统区域为 `zh-CN`（代码页 GB2312/cp936），Python 回退到系统默认编码 GBK 读取 stdin 中的 UTF-8 脚本，多字节序列被错误解释为 Unicode 代理码点，daemon 尝试以 UTF-8 编码返回结果时崩溃。

修复：`evalScript` 拆分为 `evalScript` + `runEval`，`runEval` 设置 `PYTHONIOENCODING=utf-8` 和 `PYTHONUTF8=1`；`_lib.js` 内容也进行 `sanitizeString`；daemon 崩溃自动恢复。

### 2026-07-07 修复 Qwen 导航重定向与 DeepSeek 流式生成中断

根因：(1) Qwen 首页通过 `window.location.href = '...'` 跳转到非聊天页面；(2) `closeOverlays` 在轮询期间每 5 秒执行，在 DeepSeek 上会取消生成。

修复：拦截 `Location.prototype.href` setter；移除轮询期间的 `closeOverlays` 调用；收窄 `closeOverlaps` 选择器。

### 2026-07-07 修复 GLM 思考内容混入答案

根因：`.answer-content` 包裹整条消息（思考区域 + 正式答案），`htmlToMd` 未跳过思考子树。

修复：`isInThinking` 添加 GLM 思考类名；`htmlToMd` 树遍历时跳过思考子树；早期提取增加思考内容守卫。

详细修复记录见 git history。