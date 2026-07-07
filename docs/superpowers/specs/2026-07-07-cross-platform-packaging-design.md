---
title: 跨平台单一可执行文件打包
date: 2026-07-07
status: approved
---

# 跨平台单一可执行文件打包设计

## 背景

当前项目通过 `go build` 生成单一 exe，前端已通过 `//go:embed` 内嵌。但用户体验不完整：
- 需手动启动服务
- 需手动打开浏览器访问界面
- 无托盘图标，用户难以感知程序运行状态
- 端口固定 8080，冲突时无法自动处理

## 目标

生成跨平台可执行文件，用户双击即可使用：
- Windows：`KnowledgeClip-windows.exe`
- macOS：`KnowledgeClip-macos`（Intel 和 Apple Silicon 两个版本）

## 用户体验

1. 双击运行 → 系统托盘出现图标 → 浏览器自动打开界面
2. 右键托盘图标显示菜单：
   - "打开界面" — 再次打开浏览器
   - "退出" — 关闭服务，托盘图标消失
3. 关闭浏览器标签页 ≠ 关闭服务，服务持续运行直到用户点击退出
4. 数据和配置放在程序同级目录，方便便携使用

## 技术方案

### 1. 系统托盘

使用 `github.com/getlantern/systray` 库（跨平台，支持 Windows/macOS/Linux）。

**托盘图标**：内嵌一个简单的 ICO/PNG 图标，存放在 `assets/icon.ico`。

**菜单项**：
- 标题显示实际端口：`KnowledgeClip (端口: 8080)`
- "打开界面" — 调用系统命令打开浏览器
- "退出" — 调用 `systray.Quit()`，触发服务关闭

**集成方式**：`main.go` 中调用 `systray.Run(onReady, onExit)`，`onReady` 中启动 HTTP 服务和托盘菜单。

### 2. 自动打开浏览器

服务启动后延迟 500ms（确保端口绑定成功）调用系统命令：

| 平台 | 命令 |
|------|------|
| Windows | `cmd /c start http://localhost:{port}` |
| macOS | `open http://localhost:{port}` |
| Linux | `xdg-open http://localhost:{port}` |

### 3. 端口检测与自动切换

启动时尝试绑定默认端口 8080：
- 成功：使用 8080
- 失败（端口被占用）：依次尝试 8081、8082、8083... 直到找到可用端口
- 最多尝试 10 个端口，全部失败则报错退出

实际端口返回给托盘模块，显示在菜单标题中。

### 4. 数据目录

所有数据放在程序同级目录，便携使用：

| 文件/目录 | 说明 |
|-----------|------|
| `configs/sites.yaml` | 站点配置 |
| `data/knowledgeclip.db` | SQLite 数据库 |
| `.browser-data/` | 浏览器用户数据（登录态持久化） |

首次运行自动创建这些目录。

**实现**：`main.go` 中检测目录是否存在，不存在则创建。

### 5. 交叉编译

**Windows 版本**：
```bash
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o dist/KnowledgeClip-windows.exe cmd/server/main.go
```
`-H windowsgui` 隐藏控制台窗口，纯托盘运行。

**macOS Intel 版本**：
```bash
GOOS=darwin GOARCH=amd64 go build -o dist/KnowledgeClip-macos-intel cmd/server/main.go
```

**macOS Apple Silicon 版本**：
```bash
GOOS=darwin GOARCH=arm64 go build -o dist/KnowledgeClip-macos-arm64 cmd/server/main.go
```

### 6. 构建流程

`Makefile` 新增 `cross-build` 目标：

```makefile
cross-build:
	cd web && npm run build
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o dist/KnowledgeClip-windows.exe cmd/server/main.go
	GOOS=darwin GOARCH=amd64 go build -o dist/KnowledgeClip-macos-intel cmd/server/main.go
	GOOS=darwin GOARCH=arm64 go build -o dist/KnowledgeClip-macos-arm64 cmd/server/main.go
```

## 文件结构变化

```
KnowledgeClip/
├── cmd/
│   └── server/
│       └── main.go          # 修改：集成 systray、端口检测、目录创建
├── internal/
│   ├── api/
│   │   └── server.go        # 修改：端口可配置，返回实际端口
│   └── ...
├── assets/
│   └── icon.ico             # 新增：托盘图标
├── build.sh                 # 新增：跨平台构建脚本（可选，替代 Makefile）
└── dist/                    # 新增：构建产物输出目录
```

## 依赖新增

- `github.com/getlantern/systray` — 系统托盘

## 限制与后续 TODO

- 安装包（Windows .exe 安装程序、macOS .dmg）暂不实现，后续迭代
- 托盘图标暂用简单设计，后续可替换为更精致的图标
- macOS 版本首次运行可能需要用户授权（Gatekeeper），文档中说明处理方式