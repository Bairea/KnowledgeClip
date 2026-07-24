# browser-act CLI 使用流程指南

browser-act 是一个浏览器自动化 CLI 工具，支持隐身模式和验证码绕过。本文档记录了使用该工具抓取网页内容的基本流程。

## 一、浏览器管理

### 1. 创建浏览器实例

```bash
browser-act browser create --type <type> --name <name> --desc <desc>
```

**参数说明**:
- `--type`: 浏览器类型，可选 `chrome` | `chrome-direct` | `stealth`
- `--name`: 浏览器名称（500字符上限）
- `--desc`: 描述信息（500字符上限）

**示例**:
```bash
browser-act browser create --type chrome --name dedao-test --desc "得到课程内容抓取"
```

**返回结果**:
```
id=chrome_local_105882158515617844 name="dedao-test" type=chrome
  desc="得到课程内容抓取"
```

### 2. 查看浏览器列表

```bash
browser-act browser list
```

### 3. 删除浏览器

```bash
browser-act browser delete <browser_id>
```

## 二、会话管理

### 1. 打开会话并导航

```bash
browser-act --session <session-name> browser open <browser-id> <url> [--headed]
```

**参数说明**:
- `--session`: 会话名称（必需）
- `<browser-id>`: 浏览器ID
- `<url>`: 目标网址
- `--headed`: 可选，显示浏览器窗口（默认无头模式）

**示例**:
```bash
browser-act --session dedao-session browser open chrome_local_105882158515617844 "https://www.dedao.cn/course/article?id=xxx" --headed
```

### 2. 查看活跃会话

```bash
browser-act session list
```

### 3. 关闭会话

```bash
browser-act session close <session-name>
```

**重要提示**:
- 所有浏览器操作命令都需要 `--session <name>` 参数
- 会话完成后务必关闭，避免资源泄漏

## 三、页面操作

### 1. 等待页面稳定

```bash
browser-act --session <name> wait stable [--timeout <ms>]
```

默认超时30秒（30000ms），页面稳定后返回。

### 2. 页面导航

```bash
browser-act --session <name> navigate <url>    # 导航到URL
browser-act --session <name> back               #后退
browser-act --session <name> forward            # 前进
browser-act --session <name> reload             # 刷新
```

### 3. 获取页面状态

```bash
browser-act --session <name> state              # 获取可交互元素列表（带索引号）
browser-act --session <name> screenshot [--full]# 截屏（--full 全页面）
browser-act --session <name> get title          # 获取标题
```

## 四、内容提取

### 1. 获取页面内容

```bash
browser-act --session <name> get markdown       # 获取 Markdown 格式内容
browser-act --session <name> get html           # 获取完整 HTML
browser-act --session <name> get html --selector <css>  # 获取指定元素 HTML
browser-act --session <name> get text <index>   # 获取元素文本内容
browser-act --session <name> get value <index>  # 获取输入框值
```

**推荐**: `get markdown` 输出整洁，适合后续处理。

### 2. 执行 JavaScript

```bash
browser-act --session <name> eval "document.title"
browser-act --session <name> eval --stdin       # 从 stdin 读取 JS 代码
```

## 五、页面交互

### 1. 点击元素

```bash
browser-act --session <name> click <index>      # 通过 state 返回的索引号点击
```

### 2. 输入文本

```bash
browser-act --session <name> input <index> "text"  # 点击元素后输入文本
browser-act --session <name> keys "Enter"          # 发送键盘按键
```

### 3. 滚动页面

```bash
browser-act --session <name> scroll down [--amount <px>]   # 向下滚动
browser-act --session <name> scroll up [--amount <px>]     # 向上滚动
browser-act --session <name> scrollintoview --selector <css>  # 滚动到指定元素
```

## 六、网络监控

```bash
browser-act --session <name> network requests [--filter <url>] [--type <type>] [--method <method>] [--status <code>] [--clear]
browser-act --session <name> network request <request_id>    # 查看请求详情
browser-act --session <name> network clear                   # 清除网络日志
browser-act --session <name> network offline [on|off]        # 离线模式切换
```

## 七、完整工作流示例

### 抓取网页内容的标准流程

```bash
# 1. 创建浏览器
browser-act browser create --type chrome --name my-browser --desc "内容抓取"# 获取浏览器ID，如: chrome_local_xxx

# 2. 打开会话并导航
browser-act --session my-session browser open chrome_local_xxx "https://example.com" --headed

# 3. 等待页面加载
browser-act --session my-session wait stable --timeout 30000

# 4. 获取内容
browser-act --session my-session get markdown > content.md

# 5. 关闭会话
browser-act session close my-session

# 6. （可选）删除浏览器
browser-act browser delete chrome_local_xxx
```

### 需要登录的页面处理

对于需要登录的页面，可以使用 `--headed` 参数显示浏览器窗口，手动完成登录操作后再抓取内容。

```bash
# 打开有窗口的浏览器，手动登录
browser-act --session login-session browser open chrome_local_xxx "https://example.com/login" --headed

# 手动登录完成后，等待页面稳定
browser-act --session login-session wait stable

# 导航到目标页面
browser-act --session login-session navigate "https://example.com/content"# 获取内容
browser-act --session login-session get markdown > content.md

# 关闭会话
browser-act session close login-session
```

## 八、注意事项

1. **会话所有权**: 会话名称仅在当前运行时有效，跨对话或重启后名称可能失效2. **资源清理**: 任务完成后务必关闭会话 (`session close`)，避免资源泄漏
3. **超时设置**: `wait stable` 默认30秒，复杂页面可增加超时时间
4. **输出格式**: `--format json` 可获取 JSON 格式输出，便于程序处理
5. **截图位置**: 截图默认保存到 `~/.config/browseract/screenshots/`

## 九、常用命令速查表

| 操作 | 命令 |
|------|------|
| 创建浏览器 | `browser-act browser create --type chrome --name <n> --desc <d>` |
| 打开会话 | `browser-act --session <s> browser open <id> <url> --headed` |
| 等待稳定 | `browser-act --session <s> wait stable` |
| 获取Markdown | `browser-act --session <s> get markdown` |
| 获取HTML | `browser-act --session <s> get html` |
| 获取状态 | `browser-act --session <s> state` |
| 截屏 | `browser-act --session <s> screenshot` |
| 关闭会话 | `browser-act session close <s>` |
| 查看会话 | `browser-act session list` |
| 查看浏览器 | `browser-act browser list` |

## 十、输出格式控制

```bash
browser-act --format json --session <name> get markdown   # JSON 格式输出
browser-act --format text --session <name> get markdown   # 文本格式输出（默认）
```

---

**文档版本**: 1.0
**更新日期**: 2026年7月8日
**工具版本**: 通过 `browser-act --version` 查看