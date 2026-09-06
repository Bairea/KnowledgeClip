# 迭代 2：BskEngine 重写 —— 跑在 IPC 上的并发引擎

日期：2026-09-06
状态：完成，live 验证通过
代码：`internal/engine/bsk_engine.go`（全量重写）、`internal/engine/manager.go`（路由统一）、
`internal/engine/bsk_pipeline_test.go`（新增离线管线测试）

## 决策

### D2.1 全局 mutex 已删除 —— 真并发轮询

旧引擎在整个 `evalOnTab` 期间持有引擎级 mutex，6 个站点的轮询实际串行。
新引擎：

- **求值不再持有任何引擎锁**：一次会话的求值经单条 daemon 连接多路复用
  （bskclient 保证线程安全），站点之间互不阻塞；
- 唯一的 mutex 只保护 session/tab **簿记**（map 读写、会话字段），从不跨越
  daemon 调用；
- 站点脚本无跨求值页面状态（`__PAYLOAD__`/`__KC_LIB__` 每次求值重新注入），
  不同标签页天然独立，并发安全。

### D2.2 透明会话自愈 —— 覆盖 daemon 5 分钟空闲回收

daemon 会在会话空闲 5 分钟后回收（bsk logs 可证），扩展 WebSocket 重连也会丢
会话。新引擎把 `not_found` 当作可恢复状态而非错误：

- **会话级** not_found（"session not registered..."）→ 停旧会话（幂等）→
  新会话 → 重建当前站点 tab → 原步骤重试一次；
- **标签级** not_found（"No tab with id..."）→ 只重建该站点 tab（不动会话，
  避免殃及其他站点的 tab）；
- 用户对恢复过程无感；恢复失败才向调用方报错（附原始错误）。

字符串匹配只剩一处且有界：区分上述两种 daemon 消息形态（`isTabNotFound`），
这是协议仅有的两种 not_found 文案；其余全部走结构化错误码。

### D2.3 结构化错误 → 用户可读信息

| 错误 | 旧文案 | 新文案 |
|------|--------|--------|
| 扩展掉线 | `bsk status failed: exit status 1` | "browser-skill 扩展未连接：请打开 Chrome 并确认扩展已启用（bsk doctor 可诊断）" |
| daemon 不在 | 同上 | "bsk daemon 不可用: …"（引擎会先尝试自动 `bsk daemon start`） |
| 登录墙 | `page navigated to account.minimaxi.com (login required?)` | "页面跳转到 account.minimaxi.com（站点未登录？请先在浏览器中登录该站点）" |
| 生成超时 | `answer did not stabilize after 180s (site qwen)` | "qwen 回答在 3m0s 内未稳定（站点生成过慢或页面异常）" |

### D2.4 manager 路由统一

`SendMessage`/`StartNewChat` 中 browser-act 和 bsk 的两段复制粘贴特判，统一为
`exclusiveEngines` 注册表 + `engineByName`：opt-in 扩展引擎只被显式配置的站点
使用、永不作为回退。删除无调用方的 `UseBrowserAct` 死代码。

### D2.5 测试策略：离线假 daemon 管线测试

新增 `bsk_pipeline_test.go`：假 daemon（同协议 ndjson）+ 假脚本，全离线覆盖：

- **TestBskPipelineOffline**：detect(ready) → send → wait(2 轮未稳) → extract，
  断言答案内容与 Close 时会话回收；
- **TestBskPipelineSessionRecovery**：首次求值即遭遇死会话，断言引擎自愈并
  换用新会话完成任务；
- **TestBskPipelineLoginWall**：detect 报告跳转登录域名，断言秒级失败且文案
  指向登录问题。

live 六站测试保留原门禁（`BSK_LIVE_TEST=1`）。

## 主要更改

- `bsk_engine.go`：617 行 → 重写（求值无锁、簿记锁、自愈、错误映射、
  `BSK_EVAL_TIMEOUT`/`BSK_WAIT_DEADLINE` 环境变量可调）；
- `manager.go`：`exclusiveEngines` 统一路由；
- 单脚本中的返回值解包：站点脚本 `safeStringify` 返回 JSON 字符串，
  `unwrapJSONString` 解一层。

## 验证

- 全量 `go test ./...` 通过，引擎与客户端 `-race` 十轮无告警；
- live（真实登录态）六站矩阵（2026-09-06 09:07，新引擎）：

| 站点 | 结果 | 耗时 | 提取字符 |
|------|------|------|----------|
| qwen | ✅ | 13.5s | 267 |
| kimi | ✅ | 17.7s | 236 |
| deepseek | ✅ | 11.2s | 299 |
| minimax | ✅ | 17.7s | 204 |
| glm | ✅ | 25.2s | 214 |
| doubao | ✅ | 13.8s | 190 |

**6/6 全部通过**，与旧 CLI 引擎的 ADR-0002 基线持平，且求值通道从
子进程（~10ms/次）换为 IPC（~0.7ms/次）。

## 回滚

旧 CLI 子进程引擎整体保留在 git 历史（本迭代前一版本），
`git revert` 本迭代 commit 即可整体回退，无配置迁移。
