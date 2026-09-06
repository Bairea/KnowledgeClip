# 迭代 1：bskclient —— 原生 daemon IPC 客户端替代 CLI 子进程

日期：2026-09-06 凌晨
状态：完成并已验证
代码：`internal/engine/bskclient/`（client.go / client_test.go / client_live_test.go）

## 决策

### D1.1 弃用"每次调用拉起 `bsk` CLI 子进程"，改为直连 daemon Unix socket

逆向并实测确认了 bsk 0.2.x 的 IPC 协议（`~/.bsk/run/daemon.sock`，ndjson 帧，
protocol_version 1.1）：

```
请求  {"id":"<str>","method":"tool.evaluate","params":{...}}
响应  {"id":"<str>","result":{...}}
      {"id":"<str>","error":{"code":"not_found","message":"..."}}
```

关键实测数据（本机 macOS）：

| 通道 | 单次 evaluate 延迟 | 并发能力 |
|------|-------------------|----------|
| CLI 子进程（旧方案） | ~10ms（Windows 上 200-500ms，ADR-0002 记录） | 受全局 mutex 限制，实际串行 |
| IPC 直连（新方案） | **0.7ms** | 10 并发连接 × 10 请求 = 10ms 总耗时，daemon 原生支持多路复用 |

结论：CLI 二进制只保留一个用途——daemon 未运行时执行 `bsk daemon start`；
运行时流量全部走 socket。

### D1.2 单连接多路复用 + 断线透明重连

- `Client` 持有一条连接，请求按自增字符串 id 复用（读 goroutine 路由响应）；
  写操作串行化，读并发安全。
- 连接断开时：读循环退出并 fail 所有在途请求（不悬挂）；下一次 Call 自动重拨。
- 已用 `-race` 通过 7 个假 daemon 单测：多路复用、断线在途失败、断线重连、
  错误帧→结构化 `RPCError`、页面 JS 异常→`JSError`、`session.stop` 的
  `not_found` 视为成功（幂等）。

### D1.3 错误语义分层（替代旧引擎的字符串匹配，见 P1 问题）

- daemon 级：`*RPCError{Code, Message}`，错误码常量固化在包内
  （`not_found`/`timeout`/`cdp_failed`/`no_browser_connected`/`invalid_params`…）；
- 页面级：`tool.evaluate` 返回 `ok=false` + `error.text` → `*JSError`
  （CDP 语义：JS 异常不是传输错误）；
- 调用方用 `bskclient.IsCode(err, CodeNotFound)` 精确分支，不再猜字符串。

### D1.4 协议漂移防护

`Status()` 暴露 daemon 的 `protocol_version`；本包声明 `SupportedProtocol = "1.1"`，
live 测试断言一致。协议细节全部收敛在 bskclient 单包内，未来 bsk 升级时变更面最小。

## 主要更改

- 新增 `internal/engine/bskclient`（客户端 ~430 行 + 测试 ~280 行）。
- 真实环境验证：
  - `TestLiveDaemonStatus`：真实 daemon 0.2.0 / protocol 1.1，1 个 Chrome 扩展在线；
  - `TestLiveRoundTrip`：`session start --no-focus` → `tab create example.com` →
    `evaluate document.title` 得到 "Example Domain" → `session stop`，全链路 2.4s。

## 兼容性

- socket 路径：`BSK_SOCK` 环境变量 > `$HOME/.bsk/run/daemon.sock`。
- daemon 不在时自动 `bsk daemon start`（唯一保留的 CLI 调用）。

## 遗留到下一迭代

- `BskEngine` 尚未切换到本包（迭代 2）：并发轮询、透明会话自愈、
  结构化错误 → 用户可读信息，全部落在引擎侧。
