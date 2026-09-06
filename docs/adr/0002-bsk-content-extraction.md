# ADR-0002: bsk（browser-skill CLI）作为可选扩展引擎完成内容摘取

- 状态：已采纳（可选扩展引擎；不打包、不进默认引擎链）。2026-09-06 起传输层
  重写为 daemon IPC 直连（[迭代 1-2](../iterations/)），CLI 子进程模式废弃；
  2026-09-06 起国内六站默认引擎从 browser-act 切换为 bsk
- 日期：2026-09-05（实验）→ 2026-09-05（集成验证完成）→ 2026-09-06（IPC 重构 + 默认化）
- 决策者：项目维护者

## 背景

项目的两个浏览器引擎（rod、browser-act）都依赖**自行驱动浏览器 + 页面内 JS 提取**
的方式完成"内容摘取"。其中 browser-act 需要额外维护 Python daemon、会话/浏览器记录、
编码兼容（UTF-8/GBK）、daemon 崩溃自愈等大量外围代码（见 `browser_act_engine.go`）。

我们希望验证一个更理想的方案：**用 browser-skill 的 `bsk` CLI 直接驱动用户真实
Chromium（含登录态），用 `bsk evaluate` 在页面内完成每次内容摘取，全程无 LLM 参与**。
即：摘取不再依赖"LLM 阅读页面后总结"，而是纯脚本化、确定性的 DOM→Markdown。

## 实验（2026-09-05 实测，分支内可复现）

PoC 位于 `scripts/bsk/`，仅用 5 类 bsk 命令：

1. `bsk session start --no-focus` —— 会话（每次提取独立生命周期，`session stop` 必做）；
2. `bsk tab create --url <url> --json` —— 目标页开在 Agent Window；
3. `bsk evaluate <js> --tab-id <id> --json` —— 轮询就绪、注入 prompt、等待稳定、提取；
4. `bsk get-html / snapshot` —— 兜底观察手段（本实验未用到兜底路径）；
5. `bsk session stop <id>`。

提取逻辑复用项目现有 `scripts/browser-act/lib.js` 的 `htmlToMarkdown`（经 `bsk evaluate`
原样注入页面执行），无需新写提取代码。

### 实测结果

| 场景 | 结果 |
|------|------|
| 通用页面（GitHub README，selector=article） | 4318 字符完整 Markdown（含目录树代码块、表格、CJK），~7s |
| 聊天答案全流程（Qwen 真实登录态） | 发送（Slate 编辑器注入 + Enter）→ 轮询稳定（~10s）→ 提取 189 字符，标题/代码块/表格保真 |

管线内零模型调用：bash 编排 + bsk CLI + 页面内 JS。站点侧的回答由站点自己的 LLM
生成（这是业务本身，不在本 ADR 的"无 LLM 管线"范围内）。

## 决策（已采纳）

1. **集成方式**：新增 `internal/engine/bsk_engine.go`（`BskEngine`），作为**可选扩展引擎**
   注册进 `manager.getEngines()`，但**绝不进入默认回退链**（通用 fallback 循环显式跳过
   bsk，`SendMessage`/`StartNewChat` 对 `engine_type=bsk` 的站点做独占路由），也不参与
   打包（脚本经 `getScriptsDir()` 从磁盘读取，与 browser-act 共享同一份
   `scripts/browser-act/<site>/*.js`）。前端站点配置弹窗新增 `bsk` 引擎选项。

2. **会话模型**：引擎生命周期一个 bsk session（`--no-focus`），每站点一个 agent tab；
   所有求值经 `bsk evaluate --tab-id <id>` **定向执行，不切换 active tab**，天然适合
   多站点并发（并发轮询仍用 mutex 串行化，保持确定性）。

3. **健壮性**（实测发现的两个环境问题，已修复）：
   - 扩展的 WebSocket 会周期性断开（实测 22:58 与 23:10 两次 "Connection reset"），
     daemon 自动重连但**活动 session 随之丢失**。引擎对任何 session/tab 级错误
     （含 `exit status 1`）触发 `sessionReset` + 重建会话 + 重试一次。
   - 未登录站点会把页面跳去登录域名（实测 minimax → account.minimaxi.com）。轮询
     检测到页面离开站点 host 立即失败并提示 "login required"，把 3 分钟静默等待变成
     秒级失败。

4. **站点脚本修复**：`doubao` 的 detect/send 脚本原本只认 `<textarea>`，而当前豆包
   页面是 tiptap/ProseMirror 编辑器（`div.tiptap.ProseMirror[contenteditable]`，
   `ready` 永远为 false）。已更新 detect_input.js（textarea + contenteditable 双支持）
   与 send_prompt.js（ProseMirror view dispatch → execCommand → textContent 三级
   注入 + 延迟点发送按钮）。该修复对 browser-act 引擎同样生效。

## 六站实测矩阵（2026-09-05，`BSK_LIVE_TEST=1 go test -run TestBskLiveSixSites`）

| 站点 | 结果 | 耗时 | 说明 |
|------|------|------|------|
| qwen | ✅ | ~13.5s | Slate 编辑器注入；标题/代码块/表格保真 |
| kimi | ✅ | ~15.8s | |
| deepseek | ✅ | ~11.3s | |
| minimax | ✅ | ~16s | 首次测试时浏览器无登录态（跳登录墙，4s 快速失败）；用户登录后复测通过 |
| glm | ✅ | ~25.4s | 游客模式可聊；URL 变化验证发送成功 |
| doubao | ✅ | ~13.9s | tiptap 编辑器修复后通过 |

**最终结论：6/6 全部跑通。** 测试提示词为结构化 Markdown 请求（自我介绍 + Python
代码块 + 2×2 表格），每次抽取对标题、代码块、表格的保真均验证。

## 何时选择 bsk

- 本机有 browser-skill 扩展在线（`bsk doctor` 可诊断），且希望复用用户真实浏览器
  登录态、零 cookie 持久化代码的脚本化摘取场景；
- 站点选择器失效、rod/browser-act 均失败时的替补手段（把站点 `engine.primary`
  改为 `bsk` 即锁定使用）；
- 多站点并发时 `--tab-id` 定向免切换的优势场景。

## 后果

- 正面：一次实验即证明 bsk 可独立完成"发送→等待→提取"全流程，无 LLM、无新提取
  代码（复用 lib.js 与 6 站脚本）；登录态零成本；会话级故障自愈；登录墙秒级报错。
- 负面/风险：依赖用户真实 Chromium + 扩展在线；扩展 WebSocket 周期性断开会丢会话
  （已自愈但会中断进行中的轮询 1-2 次）；每次调用有子进程 + daemon RPC 开销
  （0.2~0.5s）；会话生命周期必须显式 `session stop`（引擎 Close 保证）。
- 缓解：`bsk status` 前置检查 + 会话级错误恢复；`trap`/`Close` 保证 `session stop`。

## 参考

- 引擎：`internal/engine/bsk_engine.go`、live 测试 `internal/engine/bsk_engine_test.go`
- 编排：`manager.go`（注册 + 独占路由 + fallback 排除）、`SiteConfigModal.tsx`（选项）
- PoC：`scripts/bsk/bsk_extract.sh`、`scripts/bsk/dom_to_md.js`、`scripts/bsk/README.md`
- 样本：`scripts/bsk/out/github-readme.md`、`scripts/bsk/out/qwen-answer.md`
- 复用代码：`scripts/browser-act/lib.js`、`scripts/browser-act/<site>/*.js`
  （doubao detect/send 已适配 tiptap 编辑器）
- 相关 ADR：[ADR-0001](0001-engine-priority-rod-first.md)（rod 优先、browser-act opt-in）
## 2026-09-06 增补：传输层重写（IPC 直连）与发送自愈链

原方案的"每次调用拉起 bsk CLI 子进程 + 引擎级全局 mutex"已被替换：

1. **传输**：`internal/engine/bskclient` 直连 `~/.bsk/run/daemon.sock` 的
   ndjson JSON-RPC（协议 1.1）。单次 evaluate 0.7ms（vs CLI ~10ms），daemon
   原生多路复用，引擎级互斥锁已删除，站点真正并发轮询。CLI 仅用于 daemon 生命周期。
2. **自愈**：会话级/标签级 `not_found` 透明重建；发送失败三级抢救
   （编辑器留文→立即重发；已消费无内容→reload；仍无→重发）+ 超时提取兜底。
   动机与实测数据见 [docs/iterations/](../iterations/)。
3. **默认化**：国内六站 `engine.primary` 从 browser-act 切到 bsk（gemini 仍 cdp）。
   browser-act 引擎保留，仍可显式 opt-in。

风险更新：daemon IPC 是非公开协议（从二进制逆向 + 实测固化），bsk 升级可能
破坏；bskclient 启动时核对 `protocol_version`（当前 1.1），不匹配时报错提示升级。
