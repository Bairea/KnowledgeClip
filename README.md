# KnowledgeClip / Chat Aggregator

多站点大模型官网站点聊天聚合器。一次性输入提示词，并发推送到 Qwen / Kimi / DeepSeek 等站点，在一个界面里按站点网格展示回答，逐条切换 keep/remove，最后按 keep 过滤导出 JSON 或 Markdown。

> 技术栈：Go 1.23（gin + go-rod + playwright-go）+ React 18 + Vite + SQLite（`modernc.org/sqlite`）。单二进制部署，前端构建产物由 Go 静态托管。

## 特性

- **开箱即用**：打包后的 exe 内嵌 7 个预设站点配置（Qwen / Kimi / DeepSeek / Gemini / MiniMax / GLM / Doubao），首次运行自动创建，无需手动配置。
- 多站点配置：YAML 与 SQLite 双向同步，UI 增删改即时落库。
- 并发提问：一次输入并发推送到已勾选站点，长任务异步执行，结果通过 WebSocket 推送。
- 浏览器引擎三层降级：go-rod (CDP) → playwright-go → ts-playwright，引擎不可用时自动切到下一层。
- 会话复用：首次手动登录后 Cookie 自动持久化到 SQLite，后续直接复用会话。
- 站点自定义：URL / 选择器 / 引擎类型 / Format Prompt 全部可编辑；提供 "Auto Detect" 自动探测常见选择器。
- 会话历史：保留最近 50 条会话，可在侧边栏切换查看。
- 导出：按 keep 状态过滤后导出 JSON（含完整 session + messages）或 Markdown（按站点分节，标题自动降级以避免冲突）。

## 环境要求

- Windows 10/11（其他系统理论上可跑，但浏览器二进制需重新下载）
- Go 1.23+
- Node.js 18+（仅前端构建与开发需要）
- 一份可被 go-rod 拉取的浏览器源（默认从 `registry.npmmirror.com` 下载 Chromium）

## 快速开始

```bash
# 1. 安装前端依赖
cd web && npm install && cd ..

# 2. 一键构建（前端 + 后端，Windows GUI 单二进制）
build.bat
# 或等价命令：make build
# 产物：bin/KnowledgeClip.exe

# 3. 启动（开发模式）
make dev            # go run，默认监听 :8080
# 生产产物：双击 bin/KnowledgeClip.exe 后访问 http://localhost:8080
```

仅前端开发（前后端联调）：

```bash
# 终端 A
make dev

# 终端 B
cd web
npm run dev    # Vite dev server，/api 与 /ws 代理到 :8080
```

## 项目结构

```
.
├── cmd/server/main.go        入口：组装 DB + 配置 + 引擎 + HTTP
├── cmd/server/default_sites.yaml  预设站点配置（embed 到二进制）
├── cmd/server/embed_config.go     embed 声明文件
├── internal/
│   ├── api/                  Gin 路由 + WebSocket Hub
│   │                         （server.go / chat.go / sites.go / export.go / websocket.go）
│   ├── engine/               浏览器引擎抽象
│   │   ├── manager.go        BrowserEngine 接口 + Manager 三层降级调度
│   │   ├── rod_engine.go     第一层：go-rod（CDP）
│   │   ├── playwright_go.go  第二层：playwright-go
│   │   ├── ts_playwright.go  第三层：npx 子进程（占位）
│   │   └── detector.go       自动探测选择器
│   ├── storage/              SQLite 仓库函数（按表分文件）
│   ├── config/               sites.yaml 加载/保存
│   ├── export/               JSON / Markdown 序列化
│   └── models/               共享数据结构
├── web/                      React SPA
│   └── src/
│       ├── App.tsx           全部页面状态在此
│       ├── components/       SiteSidebar / ChatGrid / MessageCard / KeepSwitch / InputArea / ExportPanel / HistoryPanel / SiteConfigModal
│       ├── hooks/            useSites / useWebSocket
│       └── types/            Site / Session / Message
├── configs/sites.yaml        站点配置（启动时 upsert 进 SQLite）
├── docs/                     设计文档与计划
├── Makefile                  build / run / dev / clean
└── data.db                   SQLite 数据库（运行时生成）
```

## 使用流程

1. 启动后访问 `http://localhost:8080`，左栏列出默认站点（Qwen / Kimi / DeepSeek）。
2. 首次访问某站点：浏览器会自动打开该站点，按提示手动登录；登录完成后引擎会自动提取 Cookie 存入 SQLite。
3. 左栏勾选要提问的站点。
4. 底部输入框输入 prompt，发送。每个站点独立返回，UI 网格中按站点分卡显示。
5. 对每条回答点击 Keep 开关选择保留或丢弃。
6. 完成后点击右上角 "Export JSON" 或 "Export Markdown" 下载。

新增自定义站点：点击 "+ New Site" 打开配置弹窗，填入 URL 后可点 "Auto Detect" 自动填充 selectors；保存后立即生效。

## 配置说明

`configs/sites.yaml` 结构：

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
    format_prompt: ""
    cookie_file: ""

global:
  format_prompt: ""
  default_timeout: 120000   # 毫秒
  max_concurrent: 0         # 0 = 不限
```

UI 中修改站点会反向写回 YAML。两边内容**互为缓存**，避免手工编辑与 UI 状态漂移。

## API 速览

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/health` | 健康检查 |
| GET / POST | `/api/sites` | 列出 / 新增站点 |
| PUT / DELETE | `/api/sites/:id` | 更新 / 删除站点 |
| GET | `/api/detect?url=...` | 自动探测选择器 |
| POST | `/api/chat` | 提交提问，返回 `session_id`；结果通过 `/ws` 推送 |
| GET | `/api/sessions` | 最近 50 条会话 |
| GET | `/api/sessions/:id/messages` | 会话下的所有回答 |
| POST | `/api/messages/kept` | 切换 keep 状态 |
| GET | `/api/export?session_id=...&format=json\|markdown&filter_kept=true` | 导出 |
| WS | `/ws` | 实时推送 `message` / `complete` 事件 |

## 已知限制

- `ts-playwright` 引擎尚未实现，`SendMessage` 直接返回 `not yet implemented`。
- Cookie 持久化仅第一层（go-rod）实现，跨引擎切换会丢失登录态。
- 默认浏览器为 `non-headless` 模式，依赖宿主机器有图形环境（典型桌面 Windows）。
- 并发控制由全局 `max_concurrent` 配置控制；当前未实现限流逻辑，站点数过多可能拖慢机器。

## 更多信息

- 设计文档：[`docs/superpowers/specs/2026-06-25-chat-aggregator-design.md`](docs/superpowers/specs/2026-06-25-chat-aggregator-design.md)
- 实施计划：[`docs/superpowers/plans/2026-06-25-chat-aggregator.md`](docs/superpowers/plans/2026-06-25-chat-aggregator.md)
- Claude 工作指南：[`CLAUDE.md`](CLAUDE.md)
- 原始需求：[`prd.md`](prd.md)
