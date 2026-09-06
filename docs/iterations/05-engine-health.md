# 迭代 5：引擎健康状态可见化

日期：2026-09-06 下午
状态：完成，API 与侧栏徽标均已实测
代码：`internal/engine/bsk_engine.go`（Health）、`internal/engine/manager.go`（Health 聚合）、
`internal/api/health.go` + `server.go`（路由）、`web/src/hooks/useEngineStatus.ts`（新）、
`web/src/components/SiteSidebar.tsx`（徽标）

## 决策

### D5.1 状态分级：构造可用性 + bsk 深度探针

四个引擎槽位（rod/browser-act/bsk/playwright-go）统一暴露 `{name, available, detail}`：

- rod/browser-act/playwright-go：反映构造期可用性（CLI/驱动是否存在）——
  这些引擎要么可用要么完全不在，没有中间态；
- bsk：**运行时深度探针**——daemon ping（2s 超时）+ 扩展连接数 + 协议版本，
  detail 给出人话（"chrome 已连接（扩展 0.2.0）" / "浏览器扩展未连接…" /
  "daemon 未运行（发送时将自动启动…）"）。

### D5.2 探针无副作用

`BskEngine.Health` 只 ping 现有连接，**绝不**借此启动 daemon 或建会话——
状态查询不应改变系统状态；daemon 的懒启动保留给真正的发送动作。

### D5.3 徽标挂在站点旁而非全局仪表盘

用户的心智单元是"往这个站点发消息能不能成"，所以彩色小圆点（绿=可用/
红=故障/灰=未知）放在侧栏每个站点名的右侧，hover 显示 detail 全文。
60 秒轮询 + 窗口聚焦时刷新，无需手动检查。

## 验证

- `GET /api/engine/status` 实测：
  rod ✓、browser-act ✓、bsk ✓（"chrome 已连接（扩展 0.2.0）"）、
  playwright-go ✗（"未安装"）
- 侧栏渲染绿点，重试进行中的截图确认徽标与站点对齐 ✓
