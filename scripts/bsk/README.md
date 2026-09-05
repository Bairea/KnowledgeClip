# bsk 内容摘取 PoC（无 LLM 实验）

本目录是 `experiment/bsk-no-llm-extraction` 分支的实验产物：尝试**仅用 browser-skill 的
`bsk` CLI**（不经过任何 LLM）直接完成每次的内容摘取。

## 结论（2026-09-05 实测）

**可行。** 纯 bsk 调用（会话 → 开标签 → evaluate）可以完整复现项目的"发送 → 等待 →
提取"流程，提取逻辑复用项目现有的 `lib.js htmlToMarkdown`，输出 Markdown 保真（标题 /
代码块反引号+语言 / 表格分隔行）。

- 通用页面提取（GitHub README，4318 字符，含目录树代码块）：约 7s。
- 聊天答案提取全流程（Qwen 真实登录态）：发送 ~1s，回答稳定 ~10s（站点 LLM 生成，
  不在我们管线内），提取 <1s。上文见 `out/qwen-answer.md`。
- 管线内没有任何模型调用：bash + `bsk` + 页面内 JS。

## 用法

```bash
# 通用页面提取（selector 默认 article）
./bsk_extract.sh "https://github.com/Bairea/KnowledgeClip" --out out/page.md

# 聊天答案提取：先把站点页面开在 Agent Window，然后按需传 selector
# （复用 scripts/browser-act/*/extract_answer.js 的效果，见下）
```

每次执行独立生命周期，即使失败也保证 `bsk session stop`。

## 关键 bsk 原语

| 步骤 | 命令 | 说明 |
|------|------|------|
| 会话 | `bsk session start --no-focus` | 4 字母 session id |
| 开标签 | `bsk tab create --url <url> --json` | 返回 `tab_id`（Agent Window 内） |
| 轮询/发送/提取 | `bsk evaluate <js> --tab-id <id> --json` | 页面内执行 JS，返回值即结果 |
| 停止 | `bsk session stop <id>` | 必做，收尾关闭 Agent Window |

`evaluate` 支持 `--tab-id` 定向，**无需切换 active tab** —— 比 browser-act 的单
active-tab 模型更适合多站点并发。

## 从 browser-act/rod 平移到 bsk

项目现有的 `scripts/browser-act/<site>/{send_prompt,wait_answer,extract_answer}.js`
与 `lib.js` 可以直接 embed 进 `bsk evaluate` 原样运行（Qwen 已验证）。差异仅在传参：

- browser-act：`globalThis.__PAYLOAD__` 由引擎注入；
- bsk：在表达式里自己赋值 `globalThis.__PAYLOAD__ = {...};` 即可。

## 局限（记录在 ADR-0002）

- 依赖用户真实 Chromium + browser-skill 扩展在线（打包 App 的默认引擎不适合）；
- 每调用一次 `bsk` = 一次子进程 + daemon RPC（约 0.2~0.5s 开销）；
- 登录态 = 用户真实浏览器登录态，无需 cookie 持久化代码；
- `bsk record` / `snapshot` 产出的 trace 本为 LLM 驱动设计，本实验证明**不需要 LLM**
  —— 纯脚本即可驱动的确定性提取。

## 提取样本

- `out/github-readme.md` — 通用页面（GitHub README 完整内容）
- `out/qwen-answer.md` — Qwen 聊天答案（标题 + Python 代码块 + 表格）