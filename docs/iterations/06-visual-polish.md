# 迭代 6：视觉打磨 —— 夜间模式、代码块头部、空状态

日期：2026-09-06 下午
状态：完成，日/夜双模式浏览器实测
代码：`web/src/index.css`（暗色调色板）、`web/src/main.tsx`（首帧前恢复主题）、
`web/src/App.tsx`（切换按钮、空状态站点数）、`web/src/components/MessageCard.tsx`（CodeBlock）

## 决策

### D6.1 暗色 = 变量整体翻转，不是组件级适配

全部 UI 颜色都走 CSS 变量（早前的设计负债变成了这次的优势）：新增
`html[data-theme='dark']` 一组"Night Reading Room"色板——暖炭底、暖白墨、
同一 petrol 青提亮（`--accent: #5cb3b1`）、`color-scheme: dark` 让滚动条/
表单控件跟随。**没有任何组件需要条件类名**，暗色覆盖是纯 CSS。

### D6.2 主题切换：首帧前恢复，避免闪白

`main.tsx` 在 React 挂载前读 `localStorage('kc-theme')` 并设置
`documentElement.dataset.theme`（无闪烁）；头部新增「☾ 夜间 / ☀ 日间」
等宽字体按钮，与编辑室风格一致。默认跟随日间（已有用户无感知）。

### D6.3 代码块升级为带头部的一等公民

原来代码块只是一块深色高亮。现在包一层 `CodeBlock` 组件：顶栏左侧语言标签
（PYTHON/TEXT…）、右侧 copy 按钮（复制原始代码文本，2 秒 copied ✓ 反馈），
高亮器去外边框融入容器。独立组件才能持有 copied 状态（markdown components
映射里不能内联 hook）。

### D6.4 空状态回答"下一发送去哪"

空态新增一行等宽小字：`将发送到 N 个站点`（未选时提示"尚未选择站点 — 在左侧勾选"）。
空状态的核心职责是消除"我该先做什么"的疑问。

## 验证（浏览器实测截图）

- 夜间模式空态/会话视图：色板统一、健康徽标与 accent 在暗底下对比度良好 ✓
- 代码块头部：PYTHON/TEXT 标签 + COPY 按钮在暗色下渲染正确 ✓
- 切回日间、localStorage 键值 `light` 落盘 ✓
- 错误卡片（DeepSeek 旧失败）在夜间模式下错误文案 + 红色重试按钮可读 ✓
