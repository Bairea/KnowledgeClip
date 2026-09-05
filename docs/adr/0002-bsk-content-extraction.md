# ADR-0002: bsk（browser-skill CLI）直接完成内容摘取 —— 实验结论

- 状态：实验验证（分支 `experiment/bsk-no-llm-extraction`，待项目维护者决定是否采纳）
- 日期：2026-09-05
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

## 决策（实验结论）

1. **可行，且提取逻辑可直接复用**：现有 `<site>/extract_answer.js` 与 `lib.js` 不需要
   改动，embed 进 `bsk evaluate` 表达式即跑通（Qwen 已验证）。`evaluate --tab-id`
   定向执行，**不切换 active tab**，比 browser-act 的单 active-tab 模型更适合并发。

2. **不作为打包 App 的默认引擎**：bsk 依赖用户机器上安装 Chromium 并加载
   browser-skill 扩展、扩展在线连接。对"双击 exe 即可用"的桌面分发（Windows GUI
   单二进制）而言这是硬依赖，与 rod（自带 Chromium）和 browser-act（自动拉起 Chrome）
   的零配置体验冲突。适用于**本机工具链场景**（如本仓库的脚本化抓取/验证），不适用于
   端用户分发。

3. **保留为独立工具/可选集成**：本次分支交付 `scripts/bsk/`（脚本 + 提取样本 + 本
   ADR）。是否进一步集成 engine（`internal/engine/bsk_engine.go`，走 manager 显式
   opt-in，与 browser-act 同级）由维护者决定。

## 后果

- 正面：一次实验即证明 bsk 可独立完成"发送→等待→提取"全流程，无 LLM、无新提取代码；
  登录态零成本复用用户真实浏览器；`--tab-id` 免切换并发友好。
- 负面/风险：扩展与浏览器在线是硬前置（`bsk doctor` 可诊断）；每次调用有子进程 +
  daemon RPC 开销（0.2~0.5s）；会话生命周期必须显式 `session stop`（本项目引擎已习惯
  该纪律）。借用用户个人标签页会被用户侧取消（本次实验中实测到一次），读取他人/敏感
  页面不适于该路径。
- 缓解：`bsk status/doctor` 前置检查；脚本 trap EXIT 保证 `session stop`。

## 参考

- PoC：`scripts/bsk/bsk_extract.sh`、`scripts/bsk/dom_to_md.js`、`scripts/bsk/README.md`
- 样本：`scripts/bsk/out/github-readme.md`、`scripts/bsk/out/qwen-answer.md`
- 复用代码：`scripts/browser-act/lib.js`、`scripts/browser-act/qwen/*.js`
- 相关 ADR：[ADR-0001](0001-engine-priority-rod-first.md)（rod 优先、browser-act opt-in）