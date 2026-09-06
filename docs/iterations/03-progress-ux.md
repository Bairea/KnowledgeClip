# 迭代 3：全链路进度可见 + 发送失败自愈链

日期：2026-09-06 上午
状态：完成，真实站点端到端验证通过
代码：`internal/engine/progress.go`（新）、`manager.go`、`api/chat.go`、`api/websocket.go`、
`web/src/*`（进度 UI）、`internal/engine/bsk_engine.go`（自愈链）、
`scripts/browser-act/deepseek/send_prompt.js`（按钮兜底）

## 决策

### D3.1 进度事件走 context，不侵入引擎接口

引擎向 API 层的阶段上报用 `context` 携带回调（`WithProgress`/`ReportProgress`），
`BrowserEngine` 接口不变：不关心的引擎零成本，rod 与 bsk 都在四个阶段边界打点：

```
input（连接站点）→ sending（发送提问）→ generating（生成回答中）→ extracting（提取回答）
```

manager 的 `SendToSites` 增加可选 `onProgress` 回调，按站点包裹时间戳；
chat.go 转成 WS `progress` 事件广播（`{type:"progress", site_id, stage, elapsed_ms}`）。

### D3.2 前端：每卡片独立阶段步进条 + 实时秒表

MessageCard 加载时显示：四段管线步进条（已完成实心/当前呼吸/未到空心）、
中文阶段文案、每秒刷新的耗时。顶边加滑动光带。多站点并发时各卡片完全独立
（实测截图：Qwen「连接站点 1s」与 DeepSeek「生成回答中」同屏并存）。

### D3.3 发送失败自愈链（live e2e 中发现的真实故障驱动）

浏览器端到端测试暴露了新类故障：**new_chat 导航后 SPA 渲染死区**——会话已在
服务端创建（侧栏出现标题），但主区域永远不渲染内容，重发同样进入死区，
wait/extract 选择器全部落空，只有 reload 能救。三级抢救链（各至多一次）：

| 触发 | 判定 | 动作 |
|------|------|------|
| ~8s 编辑器仍留有原文 | 首发未被消费 | 立即重发 |
| ~45s 已消费但无助手内容 | 渲染死区 | `location.reload()` 后继续轮询 |
| reload 后仍无内容 | 生成丢失 | 重发一次 |
| deadline 到 | —— | 提取兜底：页面有完整文本就直接采用 |

阈值可调：`BSK_SEND_VERIFY_AFTER` / `BSK_RESEND_AFTER` / `BSK_WAIT_DEADLINE`。

### D3.4 deepseek send_prompt.js：发送后校验 + 按钮兜底

合成 Enter 在新挂载的编辑器上偶发被吞。脚本现在 800ms 后校验编辑器是否清空，
未清空则点击真实的发送按钮（`div.ds-button--primary.ds-button--circle`，
live DOM 确认过）。实测首轮发送可靠性从 ~50% 提升到 100%（本轮 DeepSeek 10.8s 一次成功）。

### D3.5 产品决策：国内六站默认引擎切到 bsk

`cmd/server/default_sites.yaml` 与运行时配置：qwen/kimi/deepseek/minimax/glm/doubao
`engine.primary: browser-act → bsk`（gemini 保持 cdp）。理由：bsk 路线是本分支
的核心（真实登录态、零 cookie 代码、IPC 并发），默认配置应与推荐路线一致。

## 验证（真实站点，浏览器实测）

- 加载态截图：两卡片独立阶段 + 秒表 ✓
- 完成态：DeepSeek 10.8s / 13.1s / 19.4s 多轮成功，Markdown（标题/表格/代码高亮）渲染保真 ✓
- Qwen 一轮遭遇渲染死区 → reload 抢救自动恢复，64s 拿到完整答案（此前该场景必失败）✓
- 全量单测 + 假 daemon 管线测试（reload 路径/重发路径/登录墙/会话恢复）通过 ✓

## 遗留

- 迭代 4（单站点重试 + 复制按钮）、5（引擎健康徽标）、6（暗色模式）未做，
  留给下一个工作窗。
- DeepSeek 偶发把 format_prompt 的格式要求复述成答案正文——站点侧行为，
  可考虑短问题时不注入格式模板。
