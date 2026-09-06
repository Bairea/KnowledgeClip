# 迭代 4：单站点重试 + 答案复制

日期：2026-09-06 下午
状态：完成，浏览器端到端验证通过
代码：`internal/api/websocket.go`、`internal/api/chat.go`（WS 带 turn）、
`web/src/App.tsx`（handleRetry/去重/进度匹配）、`MessageCard.tsx`（按钮）、`ChatGrid.tsx`

## 决策

### D4.1 重试 = 复用 /api/chat，不新建端点

单站点重试就是"对同一 session 的同一 turn 只重发一个站点"——现有 `/api/chat`
完全表达得了：`{prompt, site_ids:[site], session_id, turn}`。新增端点只会复制
一份站点解析/建消息/广播逻辑。没有后端路由变更。

### D4.2 WS 广播补带 turn（真正的后端改动）

重试的是旧轮次，而前端原先用 `currentTurnRef.current` 拼 WS 消息的卡片 id——
重试 turn 1 时会拼出错位 id。`MessageUpdate` 增加 `turn` 字段由后端回带，
前端 `msg.turn ?? currentTurnRef.current`。对正常发送是冗余信息，对重试是关键。

### D4.3 进度事件按「站点 + 加载中」匹配

原进度匹配要求 turn === 当前轮次，重试旧轮次时全部丢配。由于同一时刻只有
一个批次/重试在飞（输入框被 isLoading 锁住），按 site_id + loading 匹配无歧义。

### D4.4 历史去重：同卡片 id 保留最新行

重试会为同一 session+turn+site 落一条新 message 行（旧错误行保留，方便追溯）。
加载历史时按卡片 id 去重、保留最后一条（即重试结果）。不做 DB 级覆盖。

### D4.5 复制按钮放卡片页脚

`navigator.clipboard.writeText(content)`，2 秒"已复制 ✓"反馈。失败静默
（非安全上下文下 Clipboard API 不可用，属尽力而为）。

## 验证（浏览器实测）

- gemini（rod 引擎、无登录）超时失败 → 错误卡片出现「重试」按钮 ✓
- 点击重试 → 原卡片原位重置为「连接站点」+ 进度点，后端日志确认
  `site_ids=[gemini] session_id=<原session>` ✓
- 成功卡片的页脚出现「复制」按钮 ✓
- `tsc` 通过，全量后端测试通过 ✓
