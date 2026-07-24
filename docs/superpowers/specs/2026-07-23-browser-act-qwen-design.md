# browser-act Qwen 自动化验证设计

## 背景

`prd1.md` 要求在当前 `1_browser-act` 分支中，单独建立测试目录，验证是否可以用 `browser-act` 直接操作本地浏览器上的 Qwen 页面，完成以下能力：

1. 发送提问并返回聊天内容
2. 新建对话
3. 同一轮对话内多次提问并分别返回回答
4. 将流程沉淀为可固定调用的脚本，首版用 Python 调试，后续再评估 Go 实现

本次工作只聚焦 Qwen，不扩展到其他站点。

## 已确认事实

基于当前工作树和本地命令实测，已确认：

1. 当前分支是 `1_browser-act`
2. 本机可直接运行 `browser-act 1.0.6`
3. `browser-act` 能打开 `https://www.qianwen.com/` 并读取页面状态
4. Qwen 首页可通过 DOM 查询定位到聊天输入区域和“新建对话”按钮
5. `browser-act state` 返回的元素索引在 Qwen SPA 轻微重渲染后会失效，因此不能把索引式交互当作稳定脚本接口

结论：首版原型必须以 `browser-act eval --stdin + 固定 DOM 脚本` 为主，而不是依赖 `state/click/input` 的短期索引。

## 范围

### 本次包含

1. 在独立测试目录中创建 Python 原型脚本
2. 使用 `browser-act` 复用本地浏览器会话
3. 采用“手动登录后复用”的方式处理登录态
4. 用固定 JS 完成发问、等待、提取最后回答、新建对话、多轮追问
5. 落盘保存日志、HTML、Markdown、提取结果等证据
6. 形成是否可固定成脚本调用的结论，并记录后续 Go 化建议

### 本次不包含

1. 自动登录 Qwen
2. 接入项目主服务或替换现有 Go rod 引擎
3. 多站点统一抽象
4. 生产级错误恢复、重试编排、配置 UI

## 方案选择

### 方案 A：索引驱动

流程：`state -> click/input/get text`

问题：Qwen 发生重渲染后，索引与快照失效，命令会报 `SNAPSHOT_PAGE_CHANGED`。该方案只适合人工临时探查，不适合固定脚本。

### 方案 B：browser-act + eval + DOM 驱动

流程：用 `browser-act` 负责浏览器和会话，用 `eval --stdin` 执行固定 JS 片段完成交互和提取。

优点：

1. 不依赖易失效的 `state` 索引
2. 更接近后续 Python/Go 固定调用方式
3. DOM 逻辑可以抽为独立脚本片段，后续可复用

缺点：

1. 需要先为 Qwen 编写一组较稳定的 DOM 定位与轮询逻辑

### 结论

采用方案 B。

## 目录设计

新增目录：

```text
experiments/browser_act_qwen/
  qwen_browser_act.py
  js/
    detect_input.js
    send_prompt.js
    wait_last_answer.js
    extract_last_answer.js
    new_chat.js
  artifacts/
    <timestamp>/
      run.log
      page-before.md
      page-after.md
      last-answer.html
      last-answer.json
      screenshot.png
  notes.md
```

说明：

1. `qwen_browser_act.py` 负责命令编排、结果保存和 CLI 接口
2. `js/` 目录存放固定 DOM 逻辑，避免把长 JS 直接拼在 Python 字符串里
3. `artifacts/` 按执行时间分目录保存证据
4. `notes.md` 记录实际跑通结果、失败模式和 Go 化建议

## 交互流程设计

### 1. 打开或复用会话

Python 脚本需要支持：

1. 检查现有 browser 列表，优先复用已创建的测试 browser
2. 如不存在，则自动创建一个本地 `chrome` browser
3. 检查 session 是否存在，优先复用已有 session
4. 必要时调用 `browser open ... --headed` 打开 Qwen

目标是让首次运行容易手动登录，后续运行尽量复用已有浏览器上下文。

### 2. 手动登录后复用

首版不自动登录。脚本提供：

1. 登录等待模式：打开可见浏览器后，等待用户手动完成登录
2. 登录就绪判定：页面中可找到可输入的聊天输入框，且不处于明显登录页
3. 后续步骤默认复用该 session，不再重复要求登录

### 3. 发送提问

发送逻辑不使用 `input <index>`，而是通过 JS：

1. 查找聊天输入容器，优先匹配 `[role="textbox"]`、`[contenteditable="true"]`
2. 对输入框聚焦并写入文本
3. 触发对应的 `input`、`change`、键盘事件，保证前端状态更新
4. 优先查找发送按钮并点击
5. 若未找到可点击发送按钮，则回退到向当前焦点发送 `Enter`

### 4. 等待回答完成

等待逻辑采用“最后回答节点轮询”，而不是整页抓取：

1. 发送前记录当前回答节点数量和最后回答文本摘要
2. 发送后轮询回答容器
3. 观察最后一个 assistant 回答节点的文本变化
4. 当文本连续若干次稳定，或发送按钮恢复为可发状态时，判定本轮回答结束
5. 超时时保存页面快照和诊断日志

### 5. 提取最后回答

提取时输出两类结果：

1. **主结果**：最后一个回答节点的结构化文本或 HTML 提取结果
2. **调试证据**：
   - 整页 Markdown
   - 回答节点 HTML
   - 可选截图

提取函数只返回“最后一条回答”，避免误把历史内容当成本轮结果。

### 6. 新建对话

通过 JS 搜索包含“新建对话”文本的按钮并点击。点击后：

1. 验证旧回答区是否被清空或回到初始空态
2. 若页面仍保留旧内容，则记录失败证据
3. 新建成功后允许再次发起单轮测试

### 7. 多轮对话

同一 session 内支持按顺序发送多个 prompt：

1. 第 N 轮发送前记录当前最后回答特征
2. 提取时只返回第 N 轮新增的最后回答
3. 最终保存每轮 `prompt/answer/error/duration/artifact_path`

## Python 接口

`qwen_browser_act.py` 首版提供这些命令：

```bash
python qwen_browser_act.py open
python qwen_browser_act.py login
python qwen_browser_act.py ask "你好"
python qwen_browser_act.py multi "第一问" "第二问"
python qwen_browser_act.py new-chat
python qwen_browser_act.py probe
```

说明：

1. `open`：创建或复用 browser/session，并打开 Qwen
2. `login`：等待人工登录并验证聊天输入框可用
3. `ask`：发送单轮问题并输出最后回答
4. `multi`：同一对话中连续多轮提问
5. `new-chat`：触发新建对话并验证切换成功
6. `probe`：输出当前页面关键 DOM 探测结果，便于调试选择器

## 证据保存

每次执行写入一个新的时间戳目录，至少保存：

1. `run.log`
2. `request.json`
3. `response.json`
4. `page-before.md`
5. `page-after.md`
6. `last-answer.html`
7. `last-answer.json`
8. `screenshot.png`（在关键步骤或失败时保存）

这些证据用于回答两个问题：

1. `browser-act` 是否真的完成了提问与回答提取
2. 该流程是否已经足够稳定，可以变成固定脚本调用

## 验证方案

### 用例 1：单轮问答

输入一个短问题，例如“你好，请只回复固定短句”。

验证点：

1. 脚本能成功写入输入框并提交
2. 返回回答非空
3. `artifacts/` 中有对应日志和提取结果

### 用例 2：同一对话多轮问答

连续发送 2 到 3 个短问题。

验证点：

1. 每轮都能正常提交
2. 每轮返回的是本轮最后回答，不混入上一轮回答
3. 输出结果按轮次保存

### 用例 3：新建对话后再提问

先完成一轮问答，再执行 `new-chat`，再发送新问题。

验证点：

1. “新建对话”按钮可被定位并成功触发
2. 新对话中的回答不会混入旧对话内容
3. 脚本能继续在新对话上下文中执行

## 风险与处理

### 1. DOM 结构变化

风险：Qwen 更新前端结构后，输入框、发送按钮或回答容器选择器失效。

处理：

1. 先用较通用的属性匹配
2. 保留 `probe` 命令输出候选 DOM 信息
3. 将 DOM 片段拆为独立 JS 文件，方便快速修正

### 2. 页面重渲染导致状态失效

风险：发送前后页面 loader 切换，导致 `state` 索引不可复用。

处理：

1. 不依赖 `state` 索引作为主通路
2. 所有关键操作都在执行前重新运行 DOM 查询

### 3. 登录态失效

风险：Qwen 会话过期，需要重新登录。

处理：

1. 脚本显式区分 `open` 和 `login`
2. 登录失败时提示人工重新登录
3. 不把自动登录纳入首版目标

### 4. 回答完成判定不稳定

风险：回答流式输出过程中，脚本过早判定完成，导致截断。

处理：

1. 引入稳定轮询次数阈值
2. 保存回答长度变化过程
3. 失败时保留 `page-after.md`、`last-answer.html` 供人工比对

## Go 化建议

若 Python 原型跑通，后续 Go 实现推荐沿用相同边界：

1. Go 仅负责调用 `browser-act` CLI
2. JS 片段保持独立文件，可由 Go 读入后传给 stdin
3. Python 中的 artifact 结构和 JSON 响应格式尽量保持语言无关
4. 只有当 Python 原型已经证明 DOM 契约稳定后，再考虑把逻辑并入主项目

这意味着首版的成功标准不是“功能做进主系统”，而是“证明 browser-act + 固定脚本可以稳定完成这条链路”。

## 实施顺序

1. 写入本设计文档
2. 创建 `experiments/browser_act_qwen/` 目录结构
3. 实现 `probe/open/login`
4. 实现 `ask`
5. 实现 `multi`
6. 实现 `new-chat`
7. 实际跑通三类用例并保存 artifacts
8. 在 `notes.md` 中记录结论与 Go 化建议
