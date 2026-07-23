# browser-act 站点测试结果

## 测试环境
- browser-act: 1.0.6 (via `uv tool install browser-act-cli --python 3.12`)
- 测试日期: 2026-07-23
- 测试方式: 通过 browser-act CLI 手动执行 JS 脚本

---

## Qwen (https://www.qianwen.com/) ✅ 全流程通过

### 页面结构
- 输入框: `div[data-slate-editor="true"][role="textbox"][contenteditable="true"]`
- 发送按钮: `button[aria-label="发送消息"]`（**仅在有文本时显示**）
- 回答区域: `div[class*="qk-markdown"]`
- 新建对话: `button` 包含文本 "新建对话"

### 测试结果

| 步骤 | 状态 | 备注 |
|------|------|------|
| detect_input.js | ✅ | 正确检测到输入框 |
| send_prompt.js | ✅ | Slate.js insertText + Enter 发送 |
| wait_answer.js | ✅ | 文本稳定检测 |
| extract_answer.js | ✅ | qk-markdown 选择器有效 |
| new_chat.js | ✅ | 关键词匹配有效 |

### 关键发现
1. **发送按钮动态显示**: 输入框为空时发送按钮不存在，必须用 Enter 键兜底
2. **Slate.js 编辑器**: 必须通过 React fiber 树访问 `editor.insertText()`
3. **payload 注入**: 使用 `--stdin` 避免 PowerShell 转义问题

### 可用命令示例
```bash
# 打开 Qwen
browser-act --session test-qwen browser open <browser-id> https://www.qianwen.com/ --headed --allow-restart-chrome

# 执行 JS（必须用 --stdin）
Get-Content "scripts/browser-act/qwen/send_prompt.js" -Raw | browser-act --format json --session test-qwen eval --stdin
```

---

## Kimi (https://www.kimi.com/) ⚠️ 部分通过

### 页面结构
- 输入框: `div.chat-input-editor[role="textbox"]`
- 发送按钮: `div.send-button-container`（有 disabled 状态）
- 回答区域: 待确认（`[class*="markdown"]` 未验证）
- 新建对话: `a.new-chat-btn` 或 `div.sidebar-new-chat`

### 测试结果

| 步骤 | 状态 | 备注 |
|------|------|------|
| detect_input.js | ✅ | 正确检测到输入框 |
| send_prompt.js | ✅ | 点击 send-button-container 发送 |
| wait_answer.js | ❓ | 未验证 |
| extract_answer.js | ❌ | 未找到回答元素 |
| new_chat.js | ❌ | 点击后跳转到错误页面 |

### 问题
1. **回答提取**: 需要确认 Kimi 的回答 DOM 结构
2. **页面导航**: 点击"新建对话"按钮后跳转到会员页面而非新对话
3. **等待逻辑**: 需要验证文本稳定性检测是否有效

### 待修复
- [ ] 确认 Kimi 回答区域的选择器
- [ ] 修复新对话导航（可能需要直接导航到 chat URL）
- [ ] 验证等待逻辑

---

## DeepSeek (https://chat.deepseek.com/) ❌ 未测试

### 预期结构（基于配置）
- 输入框: `textarea`
- 发送按钮: `div.ds-button--primary`
- 回答区域: `.ds-assistant-message-main-content`

### 待验证
- [ ] detect_input.js
- [ ] send_prompt.js
- [ ] wait_answer.js
- [ ] extract_answer.js
- [ ] new_chat.js

---

## MiniMax (https://agent.minimaxi.com/) ❌ 未测试

### 预期结构（基于配置）
- 输入框: `[data-testid=message-textarea]`
- 发送按钮: `button[class*="send"]` 或 `[aria-label*="发送"]`
- 回答区域: `[class*=message-animate-in]:not(.justify-end)`

### 待验证
- [ ] detect_input.js
- [ ] send_prompt.js
- [ ] wait_answer.js
- [ ] extract_answer.js
- [ ] new_chat.js

---

## GLM (https://chatglm.cn/) ❌ 未测试

### 预期结构（基于配置）
- 输入框: `textarea`
- 发送按钮: `div.enter-icon-container`
- 回答区域: `.answer-content`

### 待验证
- [ ] detect_input.js
- [ ] send_prompt.js
- [ ] wait_answer.js
- [ ] extract_answer.js
- [ ] new_chat.js

---

## Doubao (https://www.doubao.com/chat/) ❌ 未测试

### 预期结构（基于配置）
- 输入框: `textarea`
- 发送按钮: `div[class*="send-button"]`
- 回答区域: `.md-box-root`

### 待验证
- [ ] detect_input.js
- [ ] send_prompt.js
- [ ] wait_answer.js
- [ ] extract_answer.js
- [ ] new_chat.js

---

## 通用发现

### 1. JS 执行方式
- **必须使用 `--stdin`**：直接传 JS 字符串会在 PowerShell/Bash 下出现转义问题
- **多行 JS 会被 shell 拆分**：需要写入文件后用 `Get-Content ... | browser-act eval --stdin`

### 2. 浏览器创建
```bash
# chrome 类型（导入 Chrome profile，独立 Chromium 实例）
browser-act browser create --type chrome --name "kc-qwen" --desc "Qwen browser"

# chrome-direct 类型（直接连接用户 Chrome，占用用户浏览器）
browser-act browser create --type chrome-direct --name "live" --desc "Direct attach"
```

### 3. 会话打开
```bash
browser-act --session kc-qwen browser open <browser-id> https://www.qianwen.com/ --headed --allow-restart-chrome
```

### 4. Payload 注入模式
```javascript
// 在 JS 脚本前注入 payload
globalThis.__PAYLOAD__ = Object.create(null);
var __payload__ = {"prompt":"test"};
Object.keys(__payload__).forEach(function(k) { globalThis.__PAYLOAD__[k] = __payload__[k]; });
// ... 实际脚本内容 ...
```

### 5. 跨平台注意事项
- Windows: PowerShell 会拆分多行 JS
- macOS/Linux: Bash 也有类似问题，但可以用单引号
- 统一方案: 始终使用 `--stdin`
