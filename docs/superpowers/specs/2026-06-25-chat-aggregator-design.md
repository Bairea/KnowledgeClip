# 多站点大模型官网聊天聚合器 — 设计文档

> 文档日期: 2026-06-25
> 技术栈: Go + React + SQLite
> 架构选型: 纯 Go 单体应用

---

## 1. 项目概述

一个本地运行的 Web 应用程序，将用户输入的提示词并发发送到多个大模型官网站点（Qwen、Kimi、DeepSeek 等），收集各站点的回答并在一个界面中展示。用户可以对每个回答选择保留或丢弃（Keep/Remove），支持按 keep 状态过滤后导出 JSON 或 Markdown。

### 1.1 核心需求

- 维护多个大模型官网站点配置，每个站点独立管理内容获取方式
- 一次输入并发发送到多个站点
- 每次提问自动落盘（SQLite）
- 对每个站点答案快速保留/去掉（keep/remove）
- 支持按 keep 状态过滤导出 JSON 或 Markdown
- 首次登录需用户手动操作，之后复用 session（cookie 持久化）

### 1.2 默认站点

| 站点 | URL |
|------|-----|
| Qwen | https://chat.qwen.ai |
| Kimi | https://www.kimi.com/ |
| DeepSeek | https://chat.deepseek.com/ |

---

## 2. 整体架构

### 2.1 架构选型

**纯 Go 单体应用（架构A）**

理由:
- 单二进制部署，不依赖 Python/Node 运行时
- 浏览器自动化通过多层降级保障可用性
- 后端用 Go 的高并发特性处理多站点并行请求
- 前端 React SPA 通过 Go 内嵌静态文件服务提供

```
┌─────────────────────────────────────────────────────┐
│                 浏览器 (React SPA)                    │
│  ┌──────────────────────────────────────────────┐    │
│  │  左栏: 站点列表     │  右栏: 对话网格         │    │
│  │  ┌─ Qwen     ✓ │  ┌──────┐┌──────┐┌──────┐│    │
│  │  ├─ Kimi     ✓ │  │Qwen  ││Kimi  ││Deep  ││    │
│  │  ├─ DeepSeek ✓ │  │回答  ││回答  ││回答  ││    │
│  │  └─ +添加站点  │  │[Keep]││[Keep]││[Keep]││    │
│  │                  │  └──────┘└──────┘└──────┘│    │
│  │                  │  ┌──────────────────────┐│    │
│  │                  │  │  输入框 [发送]        ││    │
│  │                  │  └──────────────────────┘│    │
│  └──────────────────────────────────────────────┘    │
└──────────────────────┬──────────────────────────────┘
                       │ HTTP API (REST) + WebSocket
┌──────────────────────▼──────────────────────────────┐
│               Go 后端 (gin/fiber)                     │
│  ┌──────────┐ ┌───────────┐ ┌────────────────────┐   │
│  │ API路由  │ │ 站点管理器 │ │ 浏览器引擎管理器    │   │
│  │          │ │ (YAML↔DB) │ │                    │   │
│  │ /chat    │ │           │ │ rod (CDP) ──┬──→ 第一│   │
│  │ /sites   │ │ 读取YAML  │ │ play-go ────┼──→ 降级│   │
│  │ /export  │ │ UI写入DB  │ │ TS Play ────┼──→ 再降│   │
│  │ /history │ │ 双向同步  │ │ 存 cookie    │   复用│   │
│  └──────────┘ └───────────┘ └────────────────────┘   │
│                    ┌──────────────┐                   │
│                    │  SQLite      │                   │
│                    │  ┌─────────┐ │                   │
│                    │  │sessions │ │                   │
│                    │  │messages │ │                   │
│                    │  │sites    │ │                   │
│                    │  └─────────┘ │                   │
│                    └──────────────┘                   │
└──────────────────────────────────────────────────────┘
```

### 2.2 技术栈

| 层级 | 技术 | 说明 |
|------|------|------|
| 前端框架 | React 18 + Vite | SPA，内嵌 Go 静态文件服务 |
| UI 组件 | Tailwind CSS | 简洁实用风格 |
| Markdown 渲染 | react-markdown + remark-gfm | 回答内容渲染 |
| 代码高亮 | prism-react-renderer | 代码块语法高亮 |
| 数学公式 | KaTeX | 数学公式渲染 |
| 后端框架 | Go + gin 或 fiber | REST API + WebSocket |
| 数据库 | SQLite + modernc.org/sqlite | 纯 Go SQLite 驱动 |
| 浏览器自动化 | rod → playwright-go → TS Playwright | 三层降级 |
| 配置 | YAML (sites.yaml) | 站点配置，双向同步 |

---

## 3. 浏览器引擎管理器（多层降级）

### 3.1 三层降级策略

```
用户发送提问
    │
    ▼
浏览器引擎管理器
    │
    ├─ [第一层] rod (Go CDP)
    │  直接通过 Chrome DevTools Protocol 控制本地 Chrome
    │  性能最好，启动最快，支持复用已有浏览器实例
    │
    ├─ [降级触发] 如果 rod 连接失败 → 尝试启动新 Chrome 实例
    │
    ├─ [第二层] playwright-go
    │  Playwright 的 Go 绑定，安装对应浏览器驱动
    │  功能更完整，但启动略慢
    │
    ├─ [降级触发] 如果 playwright-go 不可用
    │
    └─ [第三层] TS Playwright 子进程
       Go 调用 `npx playwright` 或预先安装的 Node 脚本
       通过 stdio JSON-RPC / HTTP 通信
```

### 3.2 Session 复用机制

- **首次登录**：用户首次访问某站点时，弹出提示引导手动登录
- **Cookie 持久化**：登录后浏览器引擎管理器自动提取 cookie 和 localStorage，存入 SQLite
- **后续复用**：再次访问同一站点时，自动注入持久化的 cookie，跳过登录步骤
- **失效处理**：如果 cookie 失效（如被踢出登录），提示用户重新登录

### 3.3 统一接口

所有引擎层实现相同的 `BrowserEngine` 接口：

```go
type BrowserEngine interface {
    // SendMessage 发送提示词到指定站点，返回回答内容
    SendMessage(ctx context.Context, site Site, prompt string) (string, error)
    // Close 关闭引擎，释放资源
    Close() error
}
```

### 3.4 并发隔离

- 每个站点使用独立的浏览器上下文（BrowserContext）
- 并发提问时，各站点的浏览器操作互不影响
- 某个站点超时/报错不影响其他站点

---

## 4. 数据模型

### 4.1 SQLite 表结构

```sql
-- 站点配置（由 YAML 初始化，UI 修改后同步回 YAML）
CREATE TABLE sites (
  id          TEXT PRIMARY KEY,    -- qwen, kimi, deepseek
  name        TEXT NOT NULL,       -- 显示名
  url         TEXT NOT NULL,       -- 官网URL
  engine_type TEXT NOT NULL,       -- cdp / playwright / ts-playwright
  selectors   TEXT NOT NULL,       -- JSON: {input, submit, answer, wait_for}
  cookie_file TEXT,                -- 持久化 cookie 路径
  enabled     BOOLEAN DEFAULT 1,
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 对话记录（每次提问一个 session）
CREATE TABLE sessions (
  id          TEXT PRIMARY KEY,    -- UUID
  prompt      TEXT NOT NULL,       -- 用户输入
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 各站点回答
CREATE TABLE messages (
  id          TEXT PRIMARY KEY,    -- UUID
  session_id  TEXT NOT NULL,       -- FK → sessions
  site_id     TEXT NOT NULL,       -- FK → sites
  content     TEXT NOT NULL,       -- 完整回答内容（原始格式）
  kept        BOOLEAN DEFAULT 1,   -- 默认 keep
  error       TEXT,                -- 如果获取失败
  elapsed_ms  INTEGER,             -- 耗时
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (session_id) REFERENCES sessions(id),
  FOREIGN KEY (site_id) REFERENCES sites(id)
);

-- Cookie 持久化
CREATE TABLE site_cookies (
  site_id     TEXT PRIMARY KEY,
  cookies     TEXT NOT NULL,       -- JSON 格式的 cookie 数据
  local_storage TEXT,              -- JSON 格式的 localStorage 数据
  updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (site_id) REFERENCES sites(id)
);
```

### 4.2 sites.yaml 结构

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
    cookie_file: cookies/qwen.json

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

global:
  format_prompt: |
    【回答格式要求】
    1. 使用 Markdown 格式
    2. 一级标题请从 ### 开始，不要使用 # 或 ##
    3. 代码块请标注语言
    4. 数学公式使用 $...$ 或 $$...$$
    5. 保持回答简洁，结构化
  default_timeout: 120000        # 毫秒
  max_concurrent: 0              # 0 = 不限
```

---

## 5. 并发请求与数据流

### 5.1 发送请求流程

```
用户点击「发送」
    │
    ▼
Go 后端 /chat 接口收到 prompt
    │
    ├─ 1. 创建 session 记录（SQLite）
    │
    ├─ 2. 查询左栏当前勾选的 enabled 站点
    │
    ├─ 3. 对每个勾选站点并发启动 goroutine：
    │   ┌─────────────────────────────────────┐
    │   │ goroutine (per site)                 │
    │   │                                     │
    │   │ a) 读取该站点的 format_prompt        │
    │   │ b) 合并: final_prompt = prompt +     │
    │   │    "\n\n" + site.format_prompt       │
    │   │    （如果站点无配置，使用 global）   │
    │   │                                     │
    │   │ c) 浏览器引擎管理器 → 发送           │
    │   │    ┌─ rod (主)          ← 自动降级   │
    │   │    ├─ playwright-go     ← 自动降级   │
    │   │    └─ TS Playwright     ← 自动降级   │
    │   │                                     │
    │   │ d) 写入 messages 表                  │
    │   │ e) WebSocket 推送结果给前端           │
    │   └─────────────────────────────────────┘
    │
    ├─ 4. 前端实时接收各站点结果：
    │   ┌─────────────────────────────┐
    │   │ 收到 Qwen  → Qwen 列渲染    │
    │   │ 收到 Kimi  → Kimi 列渲染    │
    │   │ 加载中...  → 骨架屏 + 旋转器 │
    │   └─────────────────────────────┘
    │
    └─ 5. 所有 goroutine 完成后，前端展示完成状态
```

### 5.2 关键设计点

- **流式推送**：每个站点结果完成即推（WebSocket），不等全部完成。用户可先查看已完成站点的回答
- **失败隔离**：某个站点超时/报错不影响其他站点，错误信息记录到 `messages.error` 字段，前端显示该站点失败
- **并发控制**：可配置最大并发数（`max_concurrent`，默认 0=不限）
- **超时管理**：每个站点单独超时（`default_timeout`，默认 120s），超时后标记失败继续等待其他站点

---

## 6. 前端页面与交互设计

### 6.1 布局方案

**左栏 + 右栏对话网格（布局A）**

```
┌──────────────────────────────────────────────────────────┐
│  多站点AI聚合  │  [+ 新站点]  │  设置  │  导出 ▼          │
├────────────┬─────────────────────────────────────────────┤
│            │  ┌──────────┬──────────┐                    │
│ ☑ Qwen     │  │ Qwen     │ Kimi     │   ← 仅勾选的站点   │
│  已登录 ✓  │  │ ┌──────┐ │ ┌──────┐ │      出现在网格    │
│            │  │ │回答  │ │ │回答  │ │                    │
│ ☑ Kimi     │  │ │内容  │ │ │内容  │ │                    │
│  已登录 ✓  │  │ │      │ │ │      │ │                    │
│            │  │ ════⚡ │ │ ════⚡ │ │                    │
│ ☐ DeepSeek │  │ [Keep] │ │ [Keep] │ │                    │
│  (未勾选)  │  └──────────┴──────────┘                    │
│            │  ┌──────────────────────────────────────┐   │
│  ────────  │  │  输入你的问题...            [发送]   │   │
│  历史记录  │  └──────────────────────────────────────┘   │
│  2026-06-23│                                           │
│  ...       │                                           │
└────────────┴─────────────────────────────────────────────┘
```

### 6.2 左栏站点列表

- 每个站点前有一个勾选框，勾选状态决定：
  1. 该站点是否在右栏显示列
  2. 发送请求时是否包含该站点
- 未勾选的站点：右栏不显示列，不发请求
- 站点旁显示登录状态（已登录 ✓ / 未登录 ✗），点击未登录站点可引导登录
- 底部有历史记录入口

### 6.3 右栏对话网格

- 等宽列并排，每个勾选站点一列
- **加载中**：骨架屏 + 旋转器
- **完成**：Markdown 渲染后的回答内容
- **失败**：红色错误提示 + 重试按钮
- **回答底部**：开关控件（KeepB），默认开（绿色），点击关闭（灰色）

### 6.4 Keep/Remove 交互（KeepB — 开关控件）

- 默认每个回答都处于 kept 状态（绿色开关）
- 用户点击开关 → 关闭（灰色）→ `messages.kept = false`
- 可随时切换，不影响其他站点
- 导出时只导出 `kept = true` 的回答

### 6.5 Markdown 渲染

- 前端使用 `react-markdown` + `remark-gfm` 渲染回答内容
- 代码块用 `prism-react-renderer` 高亮
- 数学公式用 `katex` 渲染

---

## 7. 导出功能

### 7.1 导出格式

同时支持 JSON 和 Markdown 两种格式：

- **JSON**：保留原始数据结构（session + messages 的完整原始数据），方便用户自定义处理
- **Markdown**：处理后的推荐形式，适合直接阅读和分享

### 7.2 Markdown 导出规则

**标题层级偏移（方案C）**：
- 导出结构：`# 问题` → `## 站点名`
- 站点回答内的标题自动偏移：
  - `#` → `###`
  - `##` → `####`
  - `###` → `#####`
  - 以此类推
- 代码块、数学公式等保持原样

**示例：**
```markdown
# 帮我写一个快速排序

## Qwen

### 实现思路
快速排序的核心思想是...

```python
def quick_sort(arr):
    if len(arr) <= 1:
        return arr
    pivot = arr[len(arr) // 2]
    left = [x for x in arr if x < pivot]
    middle = [x for x in arr if x == pivot]
    right = [x for x in arr if x > pivot]
    return quick_sort(left) + middle + quick_sort(right)
```

## Kimi

### 思路
快速排序是一种分治算法...
```

### 7.3 导出范围

- **当前 Session**：导出当前展示的问题和回答
- **历史范围**：选择日期范围导出多个 session
- **Keep 过滤**：导出时只包含 `kept = true` 的回答（默认启用，可关闭）

---

## 8. 站点配置管理

### 8.1 配置C：YAML + UI 双向同步

```
                     ┌──────────────┐
                     │  sites.yaml   │  ← 可分享、可版本控制
                     │   (源文件)     │
                     └──────┬───────┘
                            │ 应用启动时读入
                            ▼
                     ┌──────────────┐
                     │  sites 表     │  ← SQLite
                     │  (运行时)     │
                     └──────┬───────┘
                            │ UI 上的增删改
                            ▼
                     ┌──────────────┐
                     │  UI 编辑面板  │  ← React 组件
                     └──────┬───────┘
                            │ 编辑后同步回
                            ▼
                     ┌──────────────┐
                     │  sites.yaml   │  ← 自动覆写
                     └──────────────┘
```

### 8.2 UI 配置面板功能

- 站点列表展示（从 YAML 读取）
- 新增站点：填入 URL，系统尝试自动探测常用选择器
- 编辑站点：修改名称、URL、选择器、格式提示词
- 删除站点
- 一键导出 sites.yaml 备份
- 导入 sites.yaml 恢复

### 8.3 选择器探测

新增站点时，系统尝试自动探测页面元素选择器：
- 打开站点页面
- 扫描常见的输入框、提交按钮、回答区域选择器模式
- 提示用户验证或手动调整

---

## 9. 错误处理

### 9.1 浏览器引擎错误

| 错误类型 | 处理方式 |
|----------|----------|
| 无法连接 Chrome | 自动降级到 playwright-go |
| playwright-go 不可用 | 自动降级到 TS Playwright |
| 所有引擎不可用 | 返回错误，提示用户安装浏览器 |
| Cookie 失效 | 提示用户重新登录该站点 |
| 页面元素未找到 | 记录错误，标记该站点失败 |
| 超时（120s） | 标记超时失败，不影响其他站点 |

### 9.2 前端错误

- 网络断开：显示重连提示
- WebSocket 断开：自动重连，重连后拉取最新状态
- 导出失败：显示具体错误信息

---

## 10. 实现阶段规划

按以下顺序实现：

1. **阶段 1**：Go 后端骨架 + SQLite 数据层 + 基础 API
2. **阶段 2**：浏览器引擎管理器（rod 为主）+ 单层站点支持
3. **阶段 3**：React 前端骨架 + 左栏站点列表 + 右栏网格
4. **阶段 4**：并发请求 + WebSocket 实时推送
5. **阶段 5**：Keep/Remove 交互 + 导出功能
6. **阶段 6**：站点配置管理（YAML + UI）
7. **阶段 7**：多层降级完善（playwright-go + TS Playwright）
8. **阶段 8**：Session 复用 + Cookie 持久化
9. **阶段 9**：历史记录 + 导出范围选择
10. **阶段 10**：选择器探测 + 错误处理完善
