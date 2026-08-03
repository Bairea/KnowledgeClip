# ADR-0001: 浏览器引擎优先级 —— rod 优先，browser-act 作为显式 opt-in

- 状态：已采纳
- 日期：2026-08-03
- 决策者：项目维护者

## 背景

本项目通过 `internal/engine/manager.go` 的 `getEngines()` 构造引擎降级链，并对每个站点在 `configs/sites.yaml`（`engine.primary`）配置引擎类型。历史上 browser-act 被注册为第一优先引擎，多数站点（qwen/kimi/deepseek/minimax/glm/doubao）也在 YAML 中被显式设为 `engine.primary: browser-act`。

实测发现 browser-act 的响应速度偏慢（每次操作需拉起外部 CLI 进程，并经过 stdin/stdout 与子进程通信），在多站点并发发送时延迟明显。相比之下 rod 引擎直接通过 CDP 在进程内驱动浏览器，启动与轮询开销更小，且已实现 cookie 持久化、Slate/Lexical 编辑器输入、HTML 转 Markdown 等核心能力，足以作为默认引擎。

同时 browser-act 作为基于真实浏览器自动化的方案，在 rod 选择器失效、或目标站点反爬策略变化时仍有其兜底价值，不应被移除。因此需要重新定义引擎优先级，既让 rod 成为默认，又保留 browser-act 作为可显式选择的方案。

## 决策

1. **全局降级链顺序**：`getEngines()` 注册顺序改为 `rod -> browser-act -> playwright-go`。非 browser-act 站点（`engine.primary` 为 `cdp`/`playwright`/空串）按此顺序逐个尝试，以第一个成功的引擎为准。

2. **默认站点引擎类型**：将 qwen/kimi/deepseek/minimax/glm/doubao 在 `configs/sites.yaml` 中的 `engine.primary` 从 `browser-act` 翻转为 `cdp`，使这些站点走 rod 优先降级链，与 gemini 一致。

3. **browser-act 保留为显式 opt-in**：`engine.primary: browser-act` 的路由路径（Path A）保持不变——该站点只会使用 browser-act 引擎，不回退到 rod 或 playwright-go。若 browser-act CLI 不可用，该站点将报错失败。这是 "browser-act 仍可用" 的准确语义：**在 sites.yaml 把某站点的 `engine.primary` 设为 `browser-act`（或经站点配置弹窗选择 browser-act），即可让该站点锁定使用 browser-act 引擎。**

4. **前端暴露 browser-act 选项**：`SiteConfigModal` 的引擎类型下拉框新增 `browser-act` 选项，使该配置可经 UI 完成，与 "改站点只走 API / 双向同步" 的约定一致。

## 何时选择 browser-act

当出现以下任一情况时，建议把目标站点的 `engine.primary` 改为 `browser-act`：

- rod 的选择器在该站点失效，且短期内无法修复；
- 目标站点对 CDP 直连有检测/拦截，browser-act 基于真实用户浏览器会话更不易被识别；
- 需要复用已登录的真实浏览器实例完成一次性任务。

默认场景（大多数站点、大多数时候）应保持 `cdp`，以获得 rod 的速度与稳定性。

## 后果

- 正面：默认路径下响应更快；browser-act 仍可通过显式配置启用，能力不被削弱。
- 负面：rod 失败时会额外尝试 browser-act，引入一次 CLI 启动开销；显式配置为 browser-act 的站点不再有 rod 兜底，browser-act 不可用时直接失败。
- 缓解：browser-act 不可用会在启动时打日志告警，便于及早发现。
