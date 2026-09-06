# 2026-09-06 夜间迭代总计划（bsk 彻底重构 + 产品化迭代）

时间窗：2026-09-06 01:00 → 09:00。角色：架构师 + 产品经理 + 真实用户。

## 背景与问题清单

对现有 bsk 实现（`internal/engine/bsk_engine.go`，ADR-0002）做了一次彻底体检，
发现的问题按严重程度排序：

### P0 — 性能与并发模型错误

1. **全局 mutex 串行化所有站点**。`evalOnTab` 在整个求值期间持有 `e.mu`，
   6 个站点的轮询（每 2-3 秒一次 detect/wait/extract）全部排队。作为
   "多站并发聚合器"，这是核心路径上的架构性错误。
2. **每次调用拉起一个 `bsk` CLI 子进程**（实测 macOS ~10ms/次，Windows 上
   ADR 记录 0.2~0.5s/次）。一次完整对话 = detect 轮询 ×N + send + wait 轮询
   ×N + extract，子进程开销乘以几十次。

### P1 — 健壮性靠字符串匹配

3. `isSessionLevelErr` 靠 `strings.Contains(err, "session"/"exit status 1"/...)`
   判断是否重建会话。**任何**页面级脚本错误（exit status 1）都会误判为会话级
   故障，触发无意义的 session 重建 + 重试，掩盖真实错误。
4. **daemon 5 分钟空闲即杀 session**（bsk logs 可见 "idle session stopped"）。
   当前实现只有出错后被动重建；重建后 tab 映射全部失效，进行中的轮询丢失。

### P2 — 结构耦合

5. `siteTab`/`sanitizeString`/`truncateForLog` 定义在 `browser_act_engine.go`，
   bsk 引擎与 browser-act 引擎文件互相纠缠。
6. bsk 脚本复用 `getScriptsDir()`（browser-act 嵌入资源提取目录），语义上
   bsk 不是 browser-act 的附属品。

### P3 — 产品/UX 缺失（用户视角）

7. 回答等待期间 UI 只有一句 "awaiting response"：没有阶段感（发送中/生成中/
   提取中）、没有实时耗时。多站点并发时用户不知道哪个站点卡住了。
8. 单个站点失败无法单独重试，只能整轮重发（浪费其他站点配额）。
9. 答案没有复制按钮。
10. 引擎健康状况完全不可见：bsk 扩展掉线、browser-act CLI 缺失时，用户只有在
    发送失败后才知道。
11. 无暗色模式；卡片细节（hover、代码块头部）有打磨空间。

## 重大发现（本次重构的核心依据）

bsk daemon 在 `~/.bsk/run/daemon.sock` 暴露 Unix socket JSON-RPC（协议 1.1）。
通过二进制 strings 还原 + 实测探测，已完整验证协议：

- 帧：ndjson。请求 `{"id":"<str>","method":"...","params":{...}}`，
  响应 `{"id","result"}` 或 `{"id","error":{"code","message"}}`。
- 方法族：`system.ping/status`、`session.start/stop/list`、
  `tool.tab_create/tab_list/evaluate/...`。
- 错误码：结构化（`not_found`/`timeout`/`cdp_failed`/`no_browser_connected`/
  `invalid_params`/`protocol_error`...）；JS 异常则 `result.ok=false` + `error.text`。
- 实测性能：**IPC 直连 0.7ms/次 evaluate**（vs CLI 子进程 10ms/macOS、
  200-500ms/Windows）；**10 个并发连接 100 次求值 10ms 完成** —— daemon 天然
  支持真并发，当前引擎的全局 mutex 完全是自我设限。

因此本次"彻底重构"= **抛弃 CLI 子进程模式，改为 Go 原生 daemon IPC 客户端**。

## 迭代路线（每迭代一个 md + 一个 commit）

| # | 迭代 | 内容 | 文档 |
|---|------|------|------|
| 1 | IPC 客户端包 | `internal/engine/bskclient`：连接管理、rpc 复用、结构化错误、假 daemon 单测 | [01-ipc-client.md](01-ipc-client.md) ✅ |
| 2 | 引擎重写 | `BskEngine` 全部跑在 IPC 上：真并发轮询（每 tab 独立）、透明会话自愈（含 daemon 5min 空闲回收）、结构化错误→用户可读信息 | [02-engine-rewrite.md](02-engine-rewrite.md) ✅ |
| 2.5 | Live 验证 | 六站真实登录态全矩阵 6/6 通过 | 并入 02 文档 ✅ |
| 3 | 进度可见 + 自愈 | 引擎阶段上报 → WS → 卡片实时阶段+秒表；发送失败三级抢救链（重发/reload/提取兜底） | [03-progress-ux.md](03-progress-ux.md) ✅ |
| 4 | 操作便利 | 单站点重试按钮 + 答案一键复制 | 待下个工作窗 |
| 5 | 健康可见 | `/api/engine/status`：bsk daemon/扩展、browser-act CLI 可用性；侧栏徽标 | 待下个工作窗 |
| 6 | 视觉打磨 | 暗色模式、卡片/代码块细节、空状态 | 待下个工作窗 |
| 7 | 收尾 | ADR-0002 增补 ✅、CLAUDE.md 更新 ✅、浏览器端到端实测 ✅ | 完成 |

## 风险与对策

- **daemon 协议是非公开契约**，bsk 升级可能破坏。对策：client 启动时读
  `system.status` 的 `protocol_version`，不匹配则降级报错并提示 `bsk update`；
  协议细节集中在 bskclient 单文件，变更面最小。
- daemon 未运行时：client 自动 `bsk daemon start`（CLI 仅用于 daemon 生命周期，
  运行时零子进程）。
- 若 IPC 路线被证明不可靠，可一键回退：engine 保留构造开关
  `BSK_TRANSPORT=cli` 走旧 CLI 通道（默认 ipc）。
