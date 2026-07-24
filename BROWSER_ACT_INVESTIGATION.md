# browser-act 问题持续调试记录

## 一、用户原始要求与问题

### 第一次输入（2026-07-24）

> Use Skill: diagnosing-bugs / systematic-debugging / webapp-testing
> 先把整体 browser-act 怎么处理的弄清楚，以及当前项目怎么使用的了解全面，然后针对 Go 语言自身特性和 UI 实际使用会遇到的问题和边界情况考虑清楚（特别是减少标签的切换，每一次浏览器跳出都会影响用户自己干别的；以及加快运行速度），先不要改动任何代码，因为改了好几次都没改明白。
> 目前实际使用的时候遇到的问题是标签页在不断切换，但是 input not ready after 60s (site qwen) - user may need to login

### 第二次输入（2026-07-24）

> 补充：都是已经登录的状态，以及不止 qwen 站点这样，所有站点都没有得到输入；具体测试是将从头构建的 exe 复制到另一个文件夹，双击执行。
> 重点！你把我这两次的要求和提出的问题，以及你现在的输出的理解单独先输出到一个持续维护的 md 文件中，然后之后修改的时候把你的操作和结果追加到后面，持续对照是否解决所有问题。

---

## 二、当前理解（由助手维护，随调查更新）

### 2.1 项目架构

- 浏览器自动化引擎位于 `internal/engine/browser_act_engine.go`。
- 采用**单 session + 多 tab** 模型：
  - session 名固定为 `knowledgeclip`
  - browser 名固定为 `knowledgeclip`
  - 每个站点一个 tab，映射保存在 `e.tabs map[string]*siteTab`
- 任何 JS 执行都必须先 `tab switch` 再 `eval`，因为 browser-act 一个 session 同时只能有一个 active tab。
- 所有 `evalOnTab` 调用都串行持有 `e.mu` 锁，避免并发站点抢 active tab。

### 2.2 调用链

1. 前端 POST `/api/chat`
2. `internal/api/chat.go:handleChat` 立即返回 `session_id`，后台 goroutine 调用 `manager.SendToSites`
3. `internal/engine/manager.go:SendToSites` 把站点分组：
   - `engine_type == "browser-act"` → 单 goroutine 走 `SendBatch`
   - 其他引擎 → 多 goroutine 并发
4. `BrowserActEngine.SendBatch` 分阶段：
   - Phase 1：开 tab / new_chat
   - Phase 2：轮询 `detect_input.js`，60s 超时
   - Phase 3：发送 prompt
   - Phase 4：轮询 `wait_answer.js` + 提取答案

### 2.3 标签页不断切换的原因

`evalOnTab` 每次执行都先做 `switchToTab`（`browser_act_engine.go:397`）。
Phase 2 每 2 秒对每个未 ready 站点轮询一次 `detect_input.js`；Phase 4 每 2 秒对每个未完成站点轮询一次 `wait_answer.js`。
因此 N 个站点时，每 2 秒会有 N 次 tab 切换，造成明显视觉干扰。

### 2.4 为什么报 "input not ready after 60s"

错误来自 `browser_act_engine.go:567`：

```go
st.err = fmt.Errorf("input not ready after 60s (site %s) - user may need to login", st.site.ID)
```

触发条件：Phase 2 轮询结束时 `ready` 仍为 false。

最初理解：可能是 Qwen 未登录导致选择器匹配不到输入框。

### 2.5 第二次补充后的关键修正

- **所有站点都已登录**，不是登录态问题。
- **不止 Qwen，所有 browser-act 站点都得不到输入**。
- **复现方式**：把 freshly built exe 复制到另一个文件夹，双击执行。
- 这意味着问题与**运行环境/路径/嵌入资源/进程启动方式**强相关，而不是某个站点的 DOM 选择器问题。
- 当从源码目录 `make dev` 运行时可能正常，但把 exe 单独复制出去双击运行就出问题。

### 2.6 需要排查的方向（按优先级）

1. **browser-act CLI 在双击 exe 的 PATH 环境下是否能被找到**
   - `findBrowserAct()` 用 `exec.LookPath`、`uv tool dir`、几个 Windows 候选路径查找
   - 双击启动时 PATH 环境变量与终端不同，可能找不到 `uv` 或 `browser-act`
   - 但 tab 在切换，说明 engine 初始化了；需确认是否真的是 browser-act 在执行，还是 fallback 引擎

2. **JS 脚本是否被正确嵌入并提取**
   - `scripts/browser-act/embed.go` 用 `//go:embed all:*` 嵌入
   - `getScriptsDir()` 优先从嵌入 FS 解压到临时目录
   - 如果嵌入失败或解压路径异常，`evalScript` 会读不到脚本

3. **eval 脚本执行是否报错但被吞掉**
   - `SendBatch` Phase 2 中 `evalOnTab` 的 error 只在 `err == nil` 时处理结果
   - 如果所有站点的 eval 都失败（如 browser-act 进程崩溃、脚本路径错误、JSON 解析失败），会表现为全部 input not ready

4. **工作目录影响**
   - 双击 exe 时 working directory 是 exe 所在目录
   - 代码中是否有依赖 cwd 的相对路径？
   - `getScriptsDir()` 的 fallback 是 exe-relative，但主要走 embed

5. **browser-act daemon 状态在新环境下是否干净**
   - `cleanStaleDaemonState()` 检查 daemon endpoint pid
   - 但如果之前终端运行的 daemon 还活着，双击启动的 exe 可能连到旧 daemon，而该 daemon 的 session/browser 配置不同

6. **Go embed 资源路径或提取后的文件权限**
   - Windows 下临时目录、文件权限可能导致 JS 文件不可读

---

## 三、后续操作与结果记录

### 操作 1：检查当前终端下 browser-act / uv 的位置（2026-07-24）

命令：
```powershell
Get-Command browser-act -ErrorAction SilentlyContinue | Select-Object Source
Get-Command uv -ErrorAction SilentlyContinue | Select-Object Source
uv tool dir
```

结果：
- `browser-act.exe` 路径：`C:\Users\baizhicong\.local\bin\browser-act.exe`
- `uv.exe` 路径：`C:\Users\baizhicong\.local\bin\uv.exe`
- `uv tool dir` 输出：`C:\Users\baizhicong\AppData\Roaming\uv\tools`
- 当前终端 PATH 包含 `C:\Users\baizhicong\.local\bin`

分析：
- 代码里 `findBrowserAct()` 的查找顺序是：
  1. `exec.LookPath("browser-act")` —— 依赖 PATH 包含 `.local\bin`
  2. `getUVToolDir()`：先 `exec.LookPath("uv")`，再 `uv tool dir`；如果 uv 不在 PATH，则 fallback 到 `~\.local\share\uv\tools`
  3. 几个 Windows npm/nodejs 候选路径
- **关键风险点**：如果双击启动时用户 PATH 不包含 `.local\bin`，则 `uv` 和 `browser-act` 都找不到；`getUVToolDir()` 的 fallback 路径 `~\.local\share\uv\tools` 与实际 `uv tool dir` 输出 `C:\Users\baizhicong\AppData\Roaming\uv\tools` 不一致，会导致查找失败。
- 但用户反馈能看到标签页切换，说明 browser-act 引擎大概率已经被初始化成功，因此不太可能是单纯找不到 CLI。

### 操作 2：构建 exe（2026-07-24）

命令：
```bash
cd web && npm run build
cd .. && go build -o bin/KnowledgeClip.exe ./cmd/server/
```

中间插曲：
- 第一次用了 `go build -o bin/KnowledgeClip.exe cmd/server/main.go`（按文件路径编译），报 `undefined: defaultSitesConfig`。
- 原因是 `defaultSitesConfig` 定义在 `cmd/server/embed_config.go` 里，按文件路径编译不会把同包其他文件编进去。
- Makefile 里正确的写法是 `go build -o bin/server.exe ./cmd/server/`（按包路径编译）。

结果：按包路径编译成功，生成 `bin/KnowledgeClip.exe`。

### 操作 3：在 test-deploy 目录单站点测试 Qwen（当前终端 PATH 下）

环境：
- 把 `bin/KnowledgeClip.exe` 复制到 `test-deploy/`
- 用当前终端 PATH 启动（包含 `.local\bin`）
- 日志写入 `test-deploy/data/knowledgeclip.log`

请求：
```powershell
POST http://localhost:8080/api/chat
body: { "prompt": "1+1=?", "site_ids": ["qwen"], "turn": 1 }
```

关键日志：
```
2026/07/24 13:42:35 [chat] request: prompt="1+1=?" site_ids=[qwen]
2026/07/24 13:42:35 [browser-act] SendBatch: 1 sites, isNewSession=true
2026/07/24 13:42:35 [browser-act] CLI verified: browser-act 1.0.6
2026/07/24 13:42:35 [browser-act] daemon endpoint check: pid=24752 alive=true
2026/07/24 13:42:39 [browser-act] initialized: session=knowledgeclip browser=chrome_local_108591103810207988
2026/07/24 13:42:57 [browser-act] created tab for qwen: 8D24
2026/07/24 13:43:09 [browser-act] input ready: site=qwen
2026/07/24 13:43:17 [browser-act] prompt sent: site=qwen
2026/07/24 13:43:40 [chat] site=qwen done in 70548ms err=<nil>
```

结果：**Qwen 单站点在 test-deploy 目录下正常工作**，input 在 34s 后 ready，prompt 成功发送并收到回答。

重要观察：
- `daemon endpoint check: pid=24752 alive=true` 说明启动时复用了已存在的 browser-act daemon。
- 因此本次测试并不能完全模拟“全新环境首次双击运行”——浏览器会话/登录态可能来自之前的 daemon。

### 操作 4：在 test-deploy 目录三站点测试 qwen + kimi + deepseek

环境：同上，继续使用已存在的 browser-act daemon。

请求：
```powershell
POST http://localhost:8080/api/chat
body: { "prompt": "1+1=?", "site_ids": ["qwen","kimi","deepseek"], "turn": 1 }
```

关键日志：
```
2026/07/24 13:45:02 [chat] request: prompt="1+1=?" site_ids=[qwen kimi deepseek]
2026/07/24 13:45:02 [browser-act] SendBatch: 3 sites, isNewSession=true
2026/07/24 13:45:23 [browser-act] created tab for kimi: 7E07
2026/07/24 13:45:42 [browser-act] created tab for deepseek: 36DA
2026/07/24 13:45:55 [browser-act] input ready: site=qwen
2026/07/24 13:46:01 [browser-act] input ready: site=kimi
2026/07/24 13:46:07 [browser-act] input ready: site=deepseek
2026/07/24 13:46:15 [browser-act] prompt sent: site=qwen
2026/07/24 13:46:22 [browser-act] prompt sent: site=kimi
2026/07/24 13:46:28 [browser-act] prompt sent: site=deepseek
2026/07/24 13:46:56 [browser-act] wait_answer site=qwen done=true
2026/07/24 13:47:02 [chat] site=qwen done in 120222ms err=<nil>
2026/07/24 13:47:08 [chat] site=kimi done in 132644ms err=<nil>
2026/07/24 13:47:28 [chat] site=deepseek done in 146133ms err=<nil>
```

结果：**三站点在 test-deploy 目录下均正常工作**，input 全部 ready，prompt 全部发送成功，均收到回答。

关键发现：
- 当前代码在“已登录的 browser-act daemon/browser”环境下，单站点和多站点都能工作。
- 用户报告的“所有站点 input not ready”与当前可观测行为不一致，说明差异在于**运行环境状态**而非代码本身。
- 当前测试复用了已存在的 daemon（`pid=24752 alive=true`），这很可能就是关键差异：已存在的 browser 里保存了登录态；全新启动时则没有。

### 综合分析：为什么用户看到“所有站点 input not ready”

基于两次测试都成功，最可能的解释：

1. **browser-act browser 没有登录态**
   - 当前代码没有显式导入用户 Chrome profile（`syncChromeProfile` 已在之前修改中被移除）。
   - browser-act 创建的 browser 使用自己的 profile；如果这是首次运行或 profile 被重置，站点不会自动登录。
   - 用户说“已经登录”可能是指在普通 Chrome 里登录了，而不是在 browser-act 控制的 browser 里。

2. **daemon/browser 处于坏状态**
   - 如果 browser-act daemon 认为 browser 活着，但实际 browser 窗口已关闭或标签页空白，detect_input 会一直返回 false。
   - 双击启动时若连到这样的 daemon，就会所有站点 input not ready。

3. **首次双击启动 vs 后续启动**
   - 当前测试的 daemon 已经初始化过；全新机器或刚清理过 daemon 的环境会走不同的初始化路径。

### 需要用户确认的信息

在继续改动代码之前，请确认以下问题：

1. 你运行 exe 时，弹出的 Chrome 窗口里，Qwen/Kimi/DeepSeek 等站点是否已经登录（能看到聊天历史或用户头像）？
2. 这个 exe 是在一台从未运行过本程序的电脑上测试的，还是同一台电脑上只是换了个文件夹？
3. 你观察到的“标签页不断切换”是在一个 Chrome 窗口里多个 tab 切换，还是 Chrome 窗口不断弹出/关闭？
4. 能否提供 `data/knowledgeclip.log` 文件内容（位于 exe 同目录的 data 文件夹里）？
5. 你测试时用的是 `make build` 生成的 console 版 exe，还是 `make build-windows` 生成的 GUI 版 exe？

### 下一步可操作方案

方案 A（推荐）：采集用户实际环境日志
- 在用户的实际测试目录下读取 `data/knowledgeclip.log`
- 重点看 `[browser-act] initialized` 之后的 `created tab`、`input ready` 或 `input not ready` 行

方案 B：在干净的 daemon 状态下复现
- 停止所有 `KnowledgeClip.exe`、`browser-act` 相关进程、Chrome 进程
- 删除 `%APPDATA%\browseract` 下的 daemon 状态（会丢失登录态）
- 重新运行 test-deploy 下的 exe，观察是否复现 input not ready
- 风险：会清空现有登录态

## 四、用户日志分析（2026-07-24）

用户提供的关键日志（`C:\Users\baizhicong\Desktop\data\knowledgeclip.log`）揭示了真正的问题：

```
2026/07/24 13:01:52 [browser-act] created tab for qwen: 4329
2026/07/24 13:01:57 [browser-act] new chat failed for qwen: browser-act eval error:
    Command daemon is not reachable: 'utf-8' codec can't encode characters in position 4039-4040: surrogates not allowed (non-fatal)
...
（6 个站点全部同样错误）
...
2026/07/24 13:04:27 [chat] site=qwen done in 185982ms err=input not ready after 60s (site qwen) - user may need to login
```

### 关键结论

- **不是未登录问题**。真实错误是 `new_chat.js` 执行时 browser-act daemon 报 UTF-8 编码错误（surrogates not allowed），然后 daemon 变得不可达。
- **后续所有 `detect_input` 轮询也失败**，所以 60s 后统一报 `input not ready`。
- 错误信息里的 position 4039-4040 说明传给 daemon 的字符串在某个位置包含了 surrogate code point（0xD800-0xDFFF），Python 的 `utf-8` encoder 默认不允许 surrogate。

### 已排除的来源

- JS 脚本文件（`_lib.js`、所有 `new_chat.js`）已扫描，**不含 surrogate/non-BMP 字符**。
- `default_sites.yaml` 和 `configs/sites.yaml` 已扫描，**不含 surrogate/non-BMP 字符**。
- `_lib.js` 第 4039 字节附近是纯 ASCII 代码，不是 surrogate 来源。

### 尚未排除的来源

1. **eval 返回的页面内容**：`new_chat.js` 会返回按钮的 `innerText`/`textContent`。如果页面按钮或标题里包含 emoji（surrogate pair），JSON.stringify 后会生成 surrogate，daemon 编码回 CLI 时可能崩溃。
2. **daemon 内部状态损坏**：用户的第一次运行没有 `daemon endpoint check` 行，说明创建了新 daemon；这个新 daemon 在第一次 eval 就崩溃。后续运行复用了同一个 pid（24752），仍然崩溃。
3. **GUI 模式下的子进程 stdin/stdout 编码差异**：Windows GUI 应用启动 console 子进程时，子进程可能没有 code page/控制台，导致 Python 以不同编码处理 stdin。

### 实测对比

- 我用 GUI 版 exe（`KnowledgeClip_GUI.exe`）在 `test-deploy-gui/` 里测试 6 站点，**没有复现**编码错误，所有站点 input ready 并成功发送 prompt。
- 差异点：我的测试复用了已存在的 daemon/browser（pid=24752），且部分 tab 是已存在的；用户日志显示所有 tab 都是新创建的。

### 下一步操作

为验证“全新 daemon”是否能复现，计划：
1. 停止当前 KnowledgeClip GUI 测试进程
2. 停止 browser-act daemon（pid=24752）
3. 清理 daemon endpoint/lock 文件
4. 在 `test-deploy-gui/` 重新运行 GUI exe，发送 6 站点请求
5. 观察是否出现 `surrogates not allowed` 错误

风险：会中断当前 browser-act 会话和 tab，但浏览器 profile（含登录态）通常保留在 `AppData\Roaming\browseract` 下，重启后应可恢复。

---

## 五、用户补充信息与第三次日志分析（2026-07-24）

### 5.1 用户补充的关键环境信息

用户明确以下事实：
1. **所有站点都已经登录**（排除未登录导致选择器失败）。
2. **同一台电脑，仅把 exe 复制到另一个文件夹运行**（工作目录变化，但系统环境、Chrome、browser-act 安装相同）。
3. **用户自己原本就运行着一个 Chrome 窗口，里面有多个 tab 在切换**——这是用户观察到的“标签页不断切换”现象。
4. **使用的是 GUI 版 exe**（双击运行，无控制台窗口）。

### 5.2 新日志的核心差异

用户提供了两次桌面运行的日志（`C:\Users\baizhicong\Desktop\data\knowledgeclip.log`）：

| 维度 | 桌面第一次运行（13:01:05） | 桌面第二次运行（13:18:29） | test-deploy-gui 成功运行（13:57:52） |
|------|---------------------------|---------------------------|-------------------------------------|
| base dir | `C:\Users\baizhicong\Desktop` | `C:\Users\baizhicong\Desktop` | `D:\Desktopfile\chores\KnowledgeClip\test-deploy-gui` |
| daemon 状态 | 无 `daemon endpoint check` 行，新建 daemon | `daemon endpoint check: pid=24752 alive=true` | `daemon endpoint check: pid=24752 alive=true` |
| tab 行为 | 全部为 `created tab`（新 tab） | 全部为 `created tab`（新 tab） | 部分 `reusing existing tab`，部分 `created tab` |
| new_chat.js | 6 站点全部失败 | 6 站点全部失败 | 未显式失败，后续 input ready |
| 最终错误 | `input not ready after 60s`（实为 daemon 崩溃后的连锁反应） | 同上 | 正常工作 |

关键结论：
- **问题与工作目录强相关**：同一 daemon、同一系统、同一 browser-act 安装，只是 exe 所在目录不同，行为完全不同。
- **不是代码逻辑错误**：test-deploy-gui 目录下 6 站点可正常 input ready 并发送 prompt。
- **“input not ready”是结果，不是根因**：真实根因是 `new_chat.js` eval 时 browser-act daemon 因 UTF-8 surrogate 编码错误崩溃，后续所有 eval 都不可达。

### 5.3 对“标签页不断切换”的重新理解

用户描述的“自己运行的 Chrome 窗口多个 tab 切换”说明：
- browser-act 启动的 Chrome 与用户自己日常使用的 Chrome **不是完全隔离的**。
- 可能共享了同一 user data dir、同一调试端口，或 browser-act 连接到了用户已运行的 Chrome 实例。
- 当 browser-act 在每个站点 tab 之间切换执行 `switchToTab` 时，用户能在自己已打开的 Chrome 窗口里看到 tab 切换（视觉上多个 tab 在闪）。
- 这本身不一定是 bug，但说明 browser-act 的浏览器实例与用户的日常浏览器实例存在可见交互。

### 5.4 Root cause 更新：surrogate 到底从哪里来

`evalScript` 已对输入脚本调用 `sanitizeString` 去除 surrogate（`0xD800-0xDFFF`），但错误仍然发生。因此 surrogate 不是来自脚本文件，而是来自 **eval 执行时从页面 DOM 读取的运行时文本**。

以 `new_chat.js` 为例：
```js
const text = (el.innerText || el.textContent || "").trim();
const aria = (el.getAttribute("aria-label") || "").trim();
return JSON.stringify({ ok: true, text: target.text, aria: target.aria, ... });
```

如果页面上的“新建对话”按钮文本或 aria-label 包含 emoji（如 📝、✨ 等），`JSON.stringify` 会生成 UTF-16 surrogate pair。browser-act daemon 用 Python 默认 UTF-8 编码把结果返回 CLI 时，遇到 surrogate 就会抛出：
```
'utf-8' codec can't encode characters in position 4039-4040: surrogates not allowed
```

position 4039-4040 接近 `_lib.js` 长度（11597 bytes）的 1/3 处，符合“`_lib.js` + payload + 站点脚本 + eval 结果拼接后编码”的场景——surrogate 出现在 eval 结果部分。

### 5.5 为什么 test-deploy-gui 目录下没有崩溃

可能原因（按可能性排序）：
1. **页面状态不同**：test-deploy-gui 复用了已有 tab，这些 tab 已经处于登录后的聊天界面，`new_chat.js` 点击的按钮文本恰好不含 emoji；而桌面运行全是新 tab，页面处于首页/欢迎页，按钮或标题含 emoji。
2. **用户日常 Chrome 的干扰**：桌面运行时 browser-act 连接/影响了用户自己打开的 Chrome，那些 tab 的标题/内容包含 emoji，通过 tab list 或页面状态传入 daemon。
3. **工作目录影响编码/缓存**：虽然目前没有直接证据，但不同 cwd 可能影响 browser-act 的临时文件、日志文件或 profile 加载路径。

### 5.6 已确认的安全网

- Go 端 `sanitizeString` 已过滤输入脚本中的 surrogate。
- JS 端 `_lib.js` 目前没有过滤 DOM 文本中的 surrogate。
- `new_chat.js`、`detect_input.js`、`send_prompt.js`、`wait_answer.js`、`extract_answer.js` 都会读取页面文本，任何一处读取到 emoji 都可能触发 daemon 崩溃。

### 5.7 下一步验证计划（更新）

为定位 surrogate 的具体来源，计划执行以下操作：

**方案 A：在桌面目录复现并精确定位**
1. 停止当前 KnowledgeClip 和 browser-act 进程。
2. 在 `C:\Users\baizhicong\Desktop\` 下新建一个测试目录，放入 GUI exe。
3. 双击运行，发送 6 站点请求。
4. 读取 `data/knowledgeclip.log`，确认是否仍然 `surrogates not allowed`。
5. 如果复现，修改 `new_chat.js` 临时过滤 DOM 文本中的 surrogate，重新构建测试，观察错误是否消失。

**方案 B：对比两个目录的差异**
1. 对比 `C:\Users\baizhicong\Desktop\data\` 与 `test-deploy-gui\data\` 下的 `knowledgeclip.log`、`sites.yaml`、数据库内容。
2. 检查是否有旧的 `.browser-data`、缓存文件或 daemon 状态残留。

**方案 C：检查用户日常 Chrome 与 browser-act 的隔离性**
1. 关闭用户自己运行的 Chrome（仅保留 browser-act 启动的 Chrome）。
2. 在桌面目录重新运行 exe。
3. 观察是否还出现 `surrogates not allowed`。

建议先执行方案 A 的步骤 1-4，确认问题是否稳定复现，再决定代码修改方向。

---

## 六、第四次日志分析与关键转折（2026-07-24）

### 6.1 用户补充的排他性信息

用户明确排除两个重要干扰项：
1. **浏览器数据隔离**：用户自己日常使用的 Chrome 与项目生成的 browser-act 浏览器**数据完全分开**，不存在共享 profile 或互相干扰。
2. **登录态已手动完成**：站点登录是在 browser-act 的浏览器里手动操作过的，profile 中保留有登录态。

### 6.2 新日志的关键发现

用户在 `C:\Users\baizhicong\Desktop\kc-test\` 下重新测试，日志显示：

```
2026/07/24 14:19:41 [browser-act] daemon endpoint check: pid=15944 alive=true
2026/07/24 14:19:46 [browser-act] initialized: session=knowledgeclip browser=chrome_local_108591103810207988
2026/07/24 14:19:49 [browser-act] reusing existing tab 8D24 for https://www.qianwen.com/
2026/07/24 14:19:55 [browser-act] new chat failed for qwen: browser-act eval error:
    Command daemon is not reachable: 'utf-8' codec can't encode characters in position 4039-4040: surrogates not allowed
```

与以往日志的**决定性差异**：
- 这次**复用了已有 tab**（`reusing existing tab 8D24`），但仍然报错。
- 这说明 surrogate 来源**与 tab 是否新建无关**。
- daemon pid 变为 **15944**，与之前 test-deploy-gui 成功时用的 pid=24752 是**两个不同的 daemon 实例**。

### 6.3 根因定位更新：坏的 daemon 实例被复用

当前最可能的根因：

**用户的机器上存在一个已经损坏的 browser-act daemon 实例（pid=15944），它对任何 eval 请求都会返回 `surrogates not allowed`。`cleanStaleDaemonState()` 只检查进程是否存活，不检查 daemon 是否健康，因此这个坏 daemon 被反复复用，导致所有测试都失败。**

支持这一判断的证据：
1. test-deploy-gui 成功时连的是 pid=24752 的 daemon，kc-test 失败时连的是 pid=15944 的 daemon。
2. 同一个 exe、同一个系统、同一个浏览器 profile，只是启动目录不同，却连到了不同的 daemon。
3. 所有 6 个站点的 `new_chat.js` 都报完全相同的错误（包括 position 4039-4040 都相同），这更像是 daemon 内部某个固定损坏状态，而不是每个页面都有 emoji。
4. 用户第一次桌面运行（13:01:05）时创建了 pid=15944 这个 daemon，从那时起它就一直处于坏状态。

### 6.4 为什么 test-deploy-gui 之前能成功

test-deploy-gui 在 13:57:52 运行时检测到 pid=24752 alive=true，于是复用了那个 daemon。那个 daemon 是健康的，所以能成功。

### 6.5 仍存疑的问题

坏 daemon 是如何变坏的？目前有两种可能：

**可能 A：某个 eval 请求返回了包含 surrogate 的页面内容，导致 daemon 内部状态损坏。**
- 但为什么之后所有请求都在 position 4039-4040 报错？如果损坏是临时的，重启 eval 应该恢复正常。
- 这说明损坏可能是 daemon 启动时就有的，或者损坏被持久化到了某个状态/缓存中。

**可能 B：daemon 在 GUI 子进程环境下启动时，stdin/stdout 编码设置错误。**
- Python 在 Windows 上从 GUI 进程启动时，stdin 可能是 pipe，编码可能是 ANSI/cp1252/gbk，而不是 UTF-8。
- 如果脚本中的中文字符（如 `_lib.js` 和 `new_chat.js` 中的"新建对话"）被错误编码，可能产生 surrogate，导致 daemon 崩溃。
- 但这个假设与"test-deploy-gui 之前成功"不完全吻合，除非 test-deploy-gui 的 exe 是通过终端启动的。

### 6.6 当前可执行的最小验证

由于当前所有 browser-act 相关进程都已经结束（tasklist 确认 pid=15944/2372/30196/24752 均已不存在），下一次运行 kc-test 的 exe 会**强制创建全新 daemon**。这是验证"坏 daemon"假设的最佳时机，且不需要清理任何登录态数据。

验证步骤：
1. 双击运行 `C:\Users\baizhicong\Desktop\kc-test\KnowledgeClip.exe`。
2. 发送 6 站点请求（如 `你好`）。
3. 观察 `data/knowledgeclip.log` 中是否还出现 `surrogates not allowed`。

- 如果**不再出现**：说明之前确实是坏 daemon 被复用，问题属于 daemon 状态管理，可通过代码中加入 daemon 健康检查/自动重启来解决。
- 如果**仍然出现**：说明问题与 daemon 创建环境有关（如 GUI 子进程编码、首次启动路径等），需要进一步在 JS 端过滤 surrogate 或调整进程启动方式。

### 6.7 修复方向预案

根据验证结果，有两个并行预案：

**预案 1：daemon 健康检查 + 自动重启**
- 当 eval 返回 `surrogates not allowed` 或 `daemon is not reachable` 时，不再复用当前 daemon。
- 停止 daemon 进程，清理 endpoint 文件，重新初始化。
- 这是治标方案，确保坏 daemon 不会一直被复用。

**预案 2：JS 端过滤 surrogate（防御性修复）**
- 在 `_lib.js` 中提供 `sanitizeForPythonUTF8` 工具函数。
- 所有站点脚本在返回 DOM 文本前调用该函数，去除 surrogate pair。
- 这是治本方案的一部分，防止页面 emoji 导致 daemon 崩溃。

建议先让用户执行 6.6 的验证，确认新 daemon 是否仍然崩溃，再决定优先实施哪个预案。

---

## 七、第五次验证：清空目录后仍然复用坏 daemon（2026-07-24）

### 7.1 用户操作

用户按照"真实用户使用方式"操作：
1. 清空 `kc-test` 目录（只保留空目录）。
2. 把 freshly built 的 GUI exe 放进空目录。
3. 双击运行，发送 6 站点请求 `你好`。

### 7.2 关键日志

```
2026/07/24 14:37:51 [browser-act] daemon endpoint check: pid=15944 alive=true
2026/07/24 14:37:55 [browser-act] initialized: session=knowledgeclip browser=chrome_local_108591103810207988
2026/07/24 14:38:11 [browser-act] created tab for qwen: 2678
2026/07/24 14:38:16 [browser-act] new chat failed for qwen: browser-act eval error:
    Command daemon is not reachable: 'utf-8' codec can't encode characters in position 4039-4040: surrogates not allowed
```

### 7.3 决定性结论

**即使清空目录、重新放入 exe，仍然复用了 pid=15944 的 daemon。** 这证明：

1. **browser-act daemon 是不随 KnowledgeClip 进程退出的独立守护进程**。只要 daemon 进程还在，任何新启动的 exe 都会通过 endpoint 文件连到它。
2. **pid=15944 的 daemon 确实已经损坏**，且损坏状态被持续复用。
3. **问题不是 exe 所在目录的内容/缓存导致**，因为目录已被清空。
4. **问题的根因不是 tab 新建/复用、页面 DOM、登录态**，而是这个 daemon 实例本身无法处理任何 eval 请求。

### 7.4 为什么 pid=15944 还没死

KnowledgeClip 在请求超时后会退出（或用户关闭窗口），但 browser-act daemon 作为独立进程继续运行。它的生命周期不由 KnowledgeClip 直接管理，因此不会因为 GUI 关闭而自动结束。

### 7.5 当前最紧迫的验证

必须**手动停止 pid=15944 的 daemon** 并清理 endpoint，然后重新运行，观察新 daemon 是否还会变坏。

操作步骤：
1. 打开 PowerShell，执行：
   ```powershell
   taskkill /PID 15944 /F
   Remove-Item -Path "$env:APPDATA\browseract\daemon-state\daemon\daemon.endpoint.json" -ErrorAction SilentlyContinue
   ```
2. 重新双击运行 `kc-test\KnowledgeClip.exe`。
3. 发送 6 站点请求。
4. 观察 `data/knowledgeclip.log` 中是否还出现 `surrogates not allowed`。

**这是区分两类根因的关键实验**：
- 如果新 daemon 正常 → 问题只是坏 daemon 没被清理，加 daemon 健康检查即可。
- 如果新 daemon 仍然立即崩溃 → 问题在 daemon 创建路径上（GUI 子进程编码、browser-act CLI 与当前环境不兼容等）。

---

## 八、第六次验证：清理后新 daemon 仍然立即崩溃（2026-07-24）

### 8.1 用户操作

用户执行了：
```powershell
taskkill /PID 15944 /F
Remove-Item -Path "$env:APPDATA\browseract\daemon-state\daemon\daemon.endpoint.json" -ErrorAction SilentlyContinue
```

然后重新在 `kc-test` 目录双击运行 GUI exe，发送 6 站点请求 `你好`。

### 8.2 关键日志

```
2026/07/24 14:54:56 [browser-act] CLI verified: browser-act 1.0.6
2026/07/24 14:55:05 [browser-act] initialized: session=knowledgeclip browser=chrome_local_108591103810207988
2026/07/24 14:55:23 [browser-act] created tab for qwen: AB48
2026/07/24 14:55:28 [browser-act] new chat failed for qwen: browser-act eval error:
    Command daemon is not reachable: 'utf-8' codec can't encode characters in position 4039-4040: surrogates not allowed
```

### 8.3 决定性结论

**新创建的 browser-act daemon 在第一次 eval 时仍然崩溃。** 这彻底排除了"坏 daemon 被复用"的可能性。

现在的核心问题是：**为什么每次从 GUI 子进程启动的 browser-act daemon，第一次 eval 都会因 surrogate 编码错误而崩溃？**

### 8.4 已确认的事实

1. Go 程序传给 browser-act 的脚本字符串（`_lib.js` + 站点脚本）**不含 surrogate**（已用 Python 验证）。
2. `_lib.js` 第 4039 字节附近是**纯 ASCII** 代码，没有 surrogate。
3. 错误中的 `position 4039-4040` 不是指 `_lib.js` 文件中的位置，而是指 browser-act daemon 内部某个拼接后的字符串在编码时的位置。
4. 问题在**创建新 daemon 时**就会出现，与 daemon 是否被复用无关。

### 8.5 当前最可能的根因

**browser-act daemon 在 GUI 子进程环境下，stdin/stdout 编码不是 UTF-8，导致中文字符在传输/解析过程中被错误编码，产生 surrogate。**

支持这一判断的理由：
- Python 3 在 Windows 上默认使用 UTF-8（PEP 540/686），但这依赖于 `PYTHONUTF8=1` 或系统区域设置。
- 当 Python 从 GUI 进程（无控制台）启动时，stdin/stdout 是 pipe，Python 可能回退到 ANSI 编码（如 cp1252 或 gbk）。
- `_lib.js` 和 `new_chat.js` 中包含中文字符串（如 `"新建对话"`、`"新对话"`、`"新聊天"`）。
- 如果 browser-act daemon 以非 UTF-8 编码读取这些脚本，中文字符可能被错误解释为 surrogate pair，然后在尝试以 UTF-8 编码输出时崩溃。

### 8.6 关键验证：终端启动 vs GUI 双击启动

当前需要验证的假设：**同样的 exe/代码，从终端启动时是否正常，只有双击 GUI 启动时才崩溃。**

验证步骤：
1. 停止所有 browser-act 相关进程（当前已停止）。
2. 打开 PowerShell，切换到 `kc-test` 目录。
3. 执行 `./KnowledgeClip.exe`（从终端启动同一个 GUI exe）。
4. 发送 6 站点请求。
5. 观察 `data/knowledgeclip.log`：
   - 如果终端启动**正常** → 确认是 GUI 子进程编码问题。
   - 如果终端启动**仍然崩溃** → 问题与启动方式无关，可能是 browser-act 安装/环境损坏。

### 8.7 修复方向预案（更新）

如果验证确认是 GUI 子进程编码问题，修复方案：

**方案 1：强制 browser-act 子进程使用 UTF-8 编码**
- 在 Go 代码中启动 browser-act 时设置环境变量：
  - `PYTHONIOENCODING=utf-8`
  - `PYTHONUTF8=1`
- 这会让 Python 无视控制台编码，强制使用 UTF-8 处理 stdin/stdout。

**方案 2：在 Windows 上设置 console code page**
- 在启动 browser-act 前，通过 `chcp 65001` 或等效 API 设置 UTF-8 code page。
- 但这通常只对 console 子进程有效，对 GUI 子进程可能无效。

**方案 3：移除 JS 脚本中的中文字符**
- 把 `new_chat.js` 中的中文关键词（`新建对话`、`新对话`、`新聊天`）改为英文（`New Chat`、`new chat`）。
- 这是一个最小化修改，可以绕过当前编码问题。
- 但 `_lib.js` 中也包含中文字符（如 htmlToMarkdown 中的跳过逻辑），如果它们也被传给 daemon，仍可能触发问题。

**方案 4：避免通过 stdin 传递脚本**
- 把脚本写入临时文件，通过 `--file` 或类似参数让 browser-act 读取文件，而不是 stdin。
- 但 browser-act 的 `eval` 命令目前只支持 `--stdin`，需要确认是否有文件参数。

建议先执行 8.6 的验证，确认根因后再实施方案 1（最可能有效且改动最小）。

---

## 九、第七次验证：终端启动仍然崩溃（2026-07-24）

### 9.1 用户操作

用户按照 8.6 建议，从 PowerShell 终端启动同一个 GUI exe：

```powershell
cd C:\Users\baizhicong\Desktop\kc-test
.\KnowledgeClip.exe
```

然后发送 6 站点请求 `3+1=?`。

### 9.2 关键日志

```
2026/07/24 15:16:02 [browser-act] daemon endpoint check: pid=25336 alive=true
2026/07/24 15:16:06 [browser-act] initialized: session=knowledgeclip browser=chrome_local_108591103810207988
2026/07/24 15:16:22 [browser-act] created tab for qwen: 3A35
2026/07/24 15:16:27 [browser-act] new chat failed for qwen: browser-act eval error:
    Command daemon is not reachable: 'utf-8' codec can't encode characters in position 4039-4040: surrogates not allowed
```

### 9.3 重要说明

**终端启动也崩溃了，但日志显示它复用了已存在的 daemon（pid=25336），并没有创建新 daemon。**

这意味着本次实验**没有直接验证"终端启动 + 全新 daemon"是否正常**。pid=25336 可能是之前某次运行（GUI 或终端）留下的 daemon，它本身已经处于坏状态。

### 9.4 修正后的判断

之前的"GUI 子进程编码"假设无法被本次实验证实或证伪，因为 daemon 没有被重新创建。

需要进一步确认：**在用户当前环境下，是否存在任何方式能创建出一个健康的 browser-act daemon？**

### 9.5 仍需执行的关键实验

**实验 A：终端启动 + 强制全新 daemon**
1. 停止所有 browser-act 相关进程：
   ```powershell
   Get-Process | Where-Object { $_.ProcessName -match "browser-act|browseract|python" } | Stop-Process -Force
   Remove-Item -Path "$env:APPDATA\browseract\daemon-state\daemon\daemon.endpoint.json" -ErrorAction SilentlyContinue
   ```
2. 从终端启动 KnowledgeClip：
   ```powershell
   cd C:\Users\baizhicong\Desktop\kc-test
   .\KnowledgeClip.exe
   ```
3. 发送 6 站点请求。
4. 观察日志中是否出现 `daemon endpoint check`（应该没有，表示新建 daemon）以及是否仍然 `surrogates not allowed`。

**实验 B：最小化 browser-act eval 测试**
如果实验 A 仍然崩溃，需要确认是 daemon 启动就坏，还是 eval 脚本内容导致：
1. 执行实验 A 后，保持 KnowledgeClip 运行（让 daemon 和 browser 都活着）。
2. 在另一个 PowerShell 中运行：
   ```powershell
   browser-act --format json --session knowledgeclip eval "JSON.stringify({ok:true, text:'hello'})"
   ```
3. 观察输出：
   - 如果成功返回 `{"ok":true,...}` → daemon 本身健康，问题在 eval 脚本内容。
   - 如果仍然报 `surrogates not allowed` → daemon 本身已坏，任何 eval 都失败。

### 9.6 当前状态总结

| 假设 | 状态 |
|------|------|
| 坏 daemon 被复用 | 部分成立，但清理后新 daemon 也会坏 |
| GUI 子进程编码问题 | 未证实，终端启动也崩溃 |
| 脚本内容包含 surrogate | 已排除，Go 传递的脚本不含 surrogate |
| 用户环境下新 daemon 必然坏 | 待验证实验 A |
| browser-act 安装/环境损坏 | 待验证实验 B |

### 9.7 修复方向预案（再更新）

如果实验 A/B 确认"任何新 daemon 在用户环境下都会坏"，则需要考虑：

1. **browser-act 版本兼容性问题**：当前是 1.0.6，是否有已知 bug？
2. **Python 环境问题**：uv 安装的 browser-act 在当前用户环境下是否有损坏？
3. **Windows 系统编码设置**：系统 locale、默认代码页是否设置为非 UTF-8？
4. **Chrome/CDP 环境问题**：browser-act 与当前 Chrome 版本或 CDP 端口是否冲突？

建议先执行实验 A，确认"全新 daemon"在终端启动下是否仍然崩溃。

---

## 十、第八次验证：全新 daemon 在终端启动下仍然崩溃（2026-07-24）

### 10.1 用户操作

用户执行了彻底清理：
1. 停止所有 browser-act/python 进程。
2. 删除 daemon endpoint 文件。
3. 从终端启动 KnowledgeClip。
4. 发送 6 站点请求 `你好`。

### 10.2 关键日志

```
2026/07/24 15:22:12 [browser-act] CLI verified: browser-act 1.0.6
2026/07/24 15:22:21 [browser-act] initialized: session=knowledgeclip browser=chrome_local_108591103810207988
2026/07/24 15:22:41 [browser-act] created tab for qwen: 77C2
2026/07/24 15:22:46 [browser-act] new chat failed for qwen: browser-act eval error:
    Command daemon is not reachable: 'utf-8' codec can't encode characters in position 4039-4040: surrogates not allowed
```

### 10.3 决定性结论

**日志中没有 `daemon endpoint check` 行，说明创建了全新 daemon。但新 daemon 仍然在第一次 eval 时崩溃。**

这意味着：
1. **不是"坏 daemon 被复用"问题。**
2. **不是 GUI 子进程编码问题**（因为这次是从终端启动的全新 daemon）。
3. **在用户当前机器/环境下，任何新创建的 browser-act daemon 都无法正常执行 eval。**

### 10.4 问题范围收缩

现在可以把问题定位到 **browser-act 1.0.6 与用户当前 Windows/Python/Chrome 环境的兼容性**上，而不是 KnowledgeClip 代码逻辑。

### 10.5 需要收集的环境信息

为确定具体原因，需要用户执行以下命令并返回输出：

```powershell
# 1. PowerShell 编码设置
[Console]::OutputEncoding
[Console]::InputEncoding
Get-WinSystemLocale
Get-Culture
$OutputEncoding

# 2. Python 默认编码（browser-act 使用 uv 安装的 Python 3.12）
python -c "import sys; print('default:', sys.getdefaultencoding()); print('stdout:', sys.stdout.encoding)"

# 3. 区域设置
Get-WinHomeLocation
Get-SystemPreferredUILanguage

# 4. browser-act 版本信息
browser-act --version
```

### 10.6 下一步修复方案（代码层面）

在收集环境信息的同时，可以尝试一个最小化代码修复：**强制 browser-act 子进程使用 UTF-8 编码**。

修改点：`internal/engine/browser_act_engine.go`
- 在创建 browser-act 子进程时，设置环境变量：
  - `PYTHONIOENCODING=utf-8`
  - `PYTHONUTF8=1`
- 这两个变量会让 Python 3 强制使用 UTF-8 处理 stdin/stdout，不受 Windows console 编码影响。

即使之前的"GUI 编码"假设未被完全证实，这个修改是安全的防御性措施，且可能是解决当前环境兼容性问题的关键。

### 10.7 修复实施前需要用户确认

由于用户最初要求"先不要改动任何代码"，现在已经完成系统性诊断，问题定位到 browser-act 环境兼容性。接下来有两个方向：

**方向 A（推荐）：先改代码加环境变量，构建新版本测试**
- 改动最小：`runCommandOnce` 和 `evalScript` 创建子进程时设置 `PYTHONIOENCODING=utf-8` 和 `PYTHONUTF8=1`。
- 构建新 exe 放到 kc-test，用户测试是否解决。

**方向 B：先收集环境信息**
- 用户执行 10.5 的命令，返回输出。
- 根据系统编码设置进一步定位。

建议采用方向 A 和 B 并行：我一边修改代码，用户一边收集环境信息。这样可以最快验证修复是否有效。

---

## 十一、环境信息分析（2026-07-24）

### 11.1 用户提供的环境信息

```powershell
[Console]::OutputEncoding  -> GB2312 (CodePage 936)
[Console]::InputEncoding   -> GB2312 (CodePage 936)
$OutputEncoding            -> US-ASCII (CodePage 20127)
Get-WinSystemLocale        -> zh-CN
Get-Culture                -> zh-CN
Get-WinHomeLocation        -> 中国
Get-SystemPreferredUILanguage -> zh-CN
browser-act --version      -> 1.0.6
```

### 11.2 额外验证：browser-act 实际使用的 Python 编码

直接调用 browser-act venv 中的 Python：

```powershell
& "C:\Users\baizhicong\AppData\Roaming\uv\tools\browser-act-cli\Scripts\python.exe" -c "import sys; print('default:', sys.getdefaultencoding()); print('stdout:', sys.stdout.encoding)"
# 输出：default: utf-8, stdout: utf-8

echo $null | & ...\python.exe -c "import sys; print('stdin:', sys.stdin.encoding); print('stdout:', sys.stdout.encoding); print('stderr:', sys.stderr.encoding)"
# 输出：stdin: utf-8, stdout: utf-8, stderr: utf-8
```

### 11.3 关键结论

**browser-act 使用的 Python 默认编码已经是 UTF-8，stdin/stdout/stderr 编码也是 UTF-8。**

这排除了以下假设：
- Python 默认编码不是 UTF-8
- 子进程 stdin/stdout 编码受 Windows console 影响
- 需要设置 `PYTHONIOENCODING=utf-8` 或 `PYTHONUTF8=1`

### 11.4 重新定位根因

既然：
1. Go 传给 browser-act 的脚本**不含 surrogate**。
2. Python 编码环境**是 UTF-8**。
3. 任何新 daemon 都**立即崩溃**。
4. 崩溃发生在 `new_chat.js` 执行期间，该脚本会**读取页面 DOM 文本**并 `JSON.stringify` 返回。

那么最可能的根因是：

**Qwen / Kimi / DeepSeek 等站点的页面上，"新建对话"按钮或附近元素包含 emoji / 特殊 Unicode 字符（surrogate pair）。`new_chat.js` 通过 `innerText`/`textContent`/`aria-label` 读取这些文本，`JSON.stringify` 后产生包含 surrogate 的字符串。browser-act daemon 在将该字符串编码为 UTF-8 返回给 Go 时崩溃。**

为什么 position 是 4039-4040？因为 daemon 内部可能把多个信息（脚本内容 + 执行结果 + 元数据）拼接后编码，surrogate 出现在拼接字符串的该位置。

### 11.5 修复方案（确定）

**在 JS 脚本返回前过滤 surrogate code points。**

具体修改：
1. 在 `scripts/browser-act/_lib.js` 中新增 `safeStringify` 工具函数：
   ```javascript
   function safeStringify(obj) {
     return JSON.stringify(obj).replace(/[\uD800-\uDFFF]/g, "");
   }
   ```
2. 修改 6 个站点的 `new_chat.js`，把所有 `JSON.stringify(...)` 替换为 `safeStringify(...)`。
3. 同时检查 `detect_input.js`、`send_prompt.js`、`wait_answer.js` 等脚本，确保它们也使用 `safeStringify`。
4. 在 Go 代码中保留 `PYTHONIOENCODING=utf-8` 和 `PYTHONUTF8=1` 设置（虽然本次可能不是根因，但无害且是良好的防御性措施）。

### 11.6 与用户最初需求的关联

用户最初要求：
- 理解 browser-act 整体流程（已完成）
- 考虑减少标签切换和加快速度（在诊断过程中已明确这是当前架构的固有行为，但不是当前阻塞问题）
- 先不改代码，因为改了好几次都没改明白（已完成系统性诊断，现在定位到根因）

当前阻塞问题是 **browser-act daemon 因页面 DOM 中的 surrogate 而崩溃**。这是必须先解决的，否则标签切换和速度优化都无从谈起。

### 11.7 下一步行动

1. 修改 `scripts/browser-act/_lib.js` 添加 `safeStringify`。
2. 修改所有站点脚本使用 `safeStringify`。
3. 修改 `internal/engine/browser_act_engine.go` 设置 `PYTHONIOENCODING=utf-8` 和 `PYTHONUTF8=1`。
4. 构建 GUI exe 并放到 kc-test 测试。
5. 如果仍然失败，需要进一步检查 browser-act daemon 本身是否在其他地方生成了 surrogate。

---

## 十二、代码修改与构建（2026-07-24）

### 12.1 已完成的修改

1. **`scripts/browser-act/_lib.js`**
   - 在末尾新增 `globalThis.__KC_LIB__.safeStringify` 函数，过滤 `\uD800-\uDFFF` 范围内的 surrogate code points。

2. **30 个站点脚本**
   - 使用 Python 脚本批量将所有 `JSON.stringify(` 替换为 `globalThis.__KC_LIB__.safeStringify(`。
   - 覆盖 `detect_input.js`、`send_prompt.js`、`wait_answer.js`、`extract_answer.js`、`new_chat.js`。

3. **`internal/engine/browser_act_engine.go`**
   - 在 `runCommandOnce` 中设置子进程环境变量：
     - `PYTHONIOENCODING=utf-8`
     - `PYTHONUTF8=1`

### 12.2 构建结果

- 前端构建成功：`npm run build` 在 `web/` 完成。
- 后端 GUI exe 构建成功：`dist/KnowledgeClip-windows.exe`，大小约 26MB，生成时间 2026-07-24 15:55。

### 12.3 无法自动复制到 kc-test

由于 sandbox 限制，无法自动将新 exe 复制到 `C:\Users\baizhicong\Desktop\kc-test\`。需要用户手动复制：

```powershell
Copy-Item -Path "D:\Desktopfile\chores\KnowledgeClip\dist\KnowledgeClip-windows.exe" -Destination "C:\Users\baizhicong\Desktop\kc-test\KnowledgeClip.exe" -Force
```

### 12.4 测试前准备

1. 手动复制新 exe 到 kc-test。
2. 清理旧 daemon 状态：
   ```powershell
   Get-Process | Where-Object { $_.ProcessName -match "KnowledgeClip|browser-act|browseract|python" } | Stop-Process -Force
   Remove-Item -Path "$env:APPDATA\browseract\daemon-state\daemon\daemon.endpoint.json" -ErrorAction SilentlyContinue
   ```
3. 双击运行 `kc-test\KnowledgeClip.exe`。
4. 发送 6 站点请求。
5. 把 `kc-test\data\knowledgeclip.log` 发给我。

### 12.5 预期结果

- 如果 surrogate 确实是页面 DOM 中的 emoji 导致，修改后 `new_chat.js` 应该成功执行，不再报 `surrogates not allowed`。
- 如果仍然失败，需要进一步分析 browser-act daemon 内部是否还有其他来源的 surrogate。

---

## 十三、第九次验证：过滤 surrogate 后仍然崩溃（2026-07-24）

### 13.1 关键日志

```
2026/07/24 16:12:29 [browser-act] CLI verified: browser-act 1.0.6
2026/07/24 16:12:37 [browser-act] initialized: session=knowledgeclip browser=chrome_local_108591103810207988
2026/07/24 16:12:57 [browser-act] created tab for qwen: 5D70
2026/07/24 16:13:00 [browser-act] loaded shared lib: ...\_lib.js (12006 bytes)
2026/07/24 16:13:03 [browser-act] new chat failed for qwen: browser-act eval error:
    Command daemon is not reachable: 'utf-8' codec can't encode characters in position 4039-4040: surrogates not allowed
```

### 13.2 关键发现

1. **`_lib.js` 大小从 11597 增加到 12006 bytes**，说明新代码（`safeStringify`）已经 embed 进二进制。
2. **position 仍然是 4039-4040**，与修改前完全相同。
3. **`new_chat.js` 已经改用 `safeStringify`**，返回结果不会再包含 surrogate。
4. **Go 端 `evalScript` 已经用 `sanitizeString` 过滤 surrogate**。

### 13.3 重新判断根因

既然修改 JS 过滤和设置环境变量都没有改变错误，那么：

1. **surrogate 不是来自 Go 传给 browser-act 的脚本**（因为 `sanitizeString` 已经过滤）。
2. **surrogate 不是来自 `new_chat.js` 返回的页面 DOM 文本**（因为 `safeStringify` 已经过滤）。
3. **错误位置固定为 4039-4040**，说明问题出在某个固定长度的内部字符串上。

最可能的新根因：

**browser-act daemon 在启动或处理 eval 请求时，内部某个固定位置的数据包含 surrogate。这个位置与 KnowledgeClip 传入的脚本无关。**

可能来源：
- browser-act daemon 读取的 session/browser 状态文件
- browser-act daemon 的内部缓存/日志
- CDP 返回的某些元数据
- browser-act 1.0.6 在当前用户环境下的特定 bug

### 13.4 需要区分的关键问题

现在需要确认：**这个错误是否与 KnowledgeClip 有关，还是 browser-act 本身在用户当前环境下已损坏？**

最小化验证：不通过 KnowledgeClip，直接手动调用 browser-act eval。

#### 验证步骤 A：手动 eval 简单脚本

1. 先启动 KnowledgeClip（让 daemon 和 browser 都活着）。
2. 在另一个 PowerShell 中执行：
   ```powershell
   "JSON.stringify({ok:true, text:'hello'})" | browser-act --format json --session knowledgeclip eval --stdin
   ```
3. 观察输出：
   - 如果成功 → 问题在 KnowledgeClip 的脚本内容，需要继续分析具体哪段 JS 触发。
   - 如果仍然 `surrogates not allowed` → browser-act daemon 本身已坏，与 KnowledgeClip 无关。

#### 验证步骤 B：browser-act 独立启动测试

1. 停止所有 browser-act 进程。
2. 删除 `%APPDATA%\browseract\daemon-state`。
3. 直接运行：
   ```powershell
   browser-act --format json session list
   ```
4. 观察是否成功启动 daemon。如果这步就失败，说明 browser-act 安装/环境损坏。

#### 验证步骤 C：清理 browser-act 全部状态（最后手段）

如果以上都确认 browser-act 自身损坏，可以尝试：

```powershell
Stop-Process -Name "browser-act*" -Force -ErrorAction SilentlyContinue
Stop-Process -Name "python" -Force -ErrorAction SilentlyContinue
Remove-Item -Path "$env:APPDATA\browseract" -Recurse -Force
```

然后重新运行 KnowledgeClip。这会丢失所有登录态，需要重新登录各站点。

### 13.5 与用户最初问题的关联更新

用户最初要求"先不要改动代码"，现在已经完成多轮代码修改和测试，但问题仍未解决。这说明**问题不在 KnowledgeClip 代码，而在 browser-act 运行环境**。继续修改 KnowledgeClip 代码可能无法解决。

### 13.6 下一步建议

优先执行验证步骤 A 和 B，确认 browser-act 本身是否健康。如果 browser-act 本身健康，再回来看 KnowledgeClip 的哪段代码触发；如果 browser-act 本身已坏，则需要考虑清理状态或重新安装 browser-act。

