# 站点选择偏好持久化设计

## 背景

当前存在两个问题：

1. **删除 configs/ 目录后数据丢失**：用户删除 `configs/` 目录后重启，`createDirectories()` 检测到 `sites.yaml` 不存在，写入 embed 的默认配置，导致 SQLite 中的用户自定义站点丢失。
2. **勾选状态不持久化**：前端 `selectedSites` 是内存状态，每次启动都重置为 `enabled=true` 的站点，用户的勾选偏好（"我不常用 Qwen，默认不要勾选"）无法保存。

## 问题分析

### 当前字段语义

| 字段 | 含义 | 控制方 |
|------|------|--------|
| `enabled` | 站点配置是否完整（选择器齐全） | 后端自动计算 |

前端初始化 `selectedSites` 时读取所有 `enabled=true` 的站点，但这两个概念混淆了：
- "站点可用"（配置完整）
- "用户想用"（偏好选择）

### 场景举例

- Kimi 配置完整 → `enabled=true`，用户不想用 → 无法取消默认勾选
- 用户添加新站点但选择器不完整 → `enabled=false`，checkbox 禁用，用户无法操作
- 用户取消勾选 Qwen → 刷新页面后重新勾选

## 设计方案

### 新增字段

| 字段 | 类型 | 默认值 | 含义 | 控制方 |
|------|------|--------|------|--------|
| `selected` | bool | true | 用户是否默认勾选该站点 | 用户在 UI 中切换 |

### 字段语义对照

| 字段 | 含义 | 用途 |
|------|------|------|
| `enabled` | 站点配置是否完整（input/submit/answer 选择器齐全） | 控制 checkbox 是否可点击 |
| `selected` | 用户是否默认勾选该站点 | 控制 checkbox 默认选中状态 |

两个维度独立：
- `enabled=true, selected=true`：配置完整且用户想用 → 默认勾选
- `enabled=true, selected=false`：配置完整但用户不想用 → 默认不勾选
- `enabled=false, selected=true`：配置不完整但用户想用 → checkbox 禁用（等待用户完善配置）
- `enabled=false, selected=false`：配置不完整且用户不想用 → checkbox 禁用

### 交互行为

1. **checkbox 可点击条件**：`enabled=true`
2. **checkbox 默认选中状态**：`selected=true`
3. **checkbox 点击行为**：调用 API 更新 `selected` 字段
4. **新增站点**：`selected` 默认为 `true`
5. **前端初始化**：`selectedSites` 从 `selected=true` 的站点读取

### 问题一修复：启动逻辑

**当前逻辑：**
```
configs/sites.yaml 不存在 → 写入 embed 默认配置
```

**修复后逻辑：**
```
1. 检查 SQLite sites 表是否有数据
2. 有数据 → 从 SQLite 写入 configs/sites.yaml，跳过 embed 写入
3. 无数据 且 configs/sites.yaml 不存在 → 写入 embed 默认配置
```

**实现细节：**

在 `cmd/server/main.go` 的 `createDirectories` 中：

```go
func createDirectories() {
    // 创建目录...

    configPath := filepath.Join("configs", "sites.yaml")
    configExists := false
    if _, err := os.Stat(configPath); err == nil {
        configExists = true
    }

    // 检查 SQLite 是否有数据
    hasSites, err := storage.HasSites(db)
    if err != nil {
        log.Fatalf("check sites: %v", err)
    }

    if hasSites {
        // SQLite 有数据，恢复 YAML
        sites, err := storage.GetSites(db)
        if err != nil {
            log.Fatalf("get sites: %v", err)
        }
        cfg := &config.Config{}
        for _, site := range sites {
            // 转换为 SiteConfig...
        }
        if err := config.Save(configPath, cfg); err != nil {
            log.Fatalf("save config: %v", err)
        }
        fmt.Println("Restored config from database: configs/sites.yaml")
    } else if !configExists {
        // SQLite 无数据且 YAML 不存在，写入 embed 默认配置
        err := os.WriteFile(configPath, defaultSitesConfig, 0644)
        if err != nil {
            log.Fatalf("create default config: %v", err)
        }
        fmt.Println("Created default config with preset sites: configs/sites.yaml")
    }
}
```

**注意：** 需要将 `createDirectories` 移到 `storage.NewDB` 之后调用，或者拆分为两个函数。

## 改动范围

### 数据库

`internal/storage/db.go`：

```sql
ALTER TABLE sites ADD COLUMN selected INTEGER DEFAULT 1
```

### 后端模型

`internal/models/models.go`：

```go
type Site struct {
    ID           string
    Name         string
    URL          string
    EngineType   string
    Selectors    string
    CookieFile   string
    Enabled      bool
    Selected     bool    // 新增
    FormatPrompt string
    CreatedAt    time.Time
}
```

### 后端存储层

`internal/storage/site_store.go`：

- `SyncSites`：upsert 时包含 `selected` 字段
- `SaveSite`：insert 时包含 `selected` 字段
- `UpdateSite`：update 时包含 `selected` 字段
- `GetSites` / `GetSiteByID`：scan 时包含 `selected` 字段
- 新增 `HasSites(db *DB) (bool, error)`：检查 sites 表是否有数据

`internal/storage/db.go`：

- `NewDB`：添加 `ALTER TABLE sites ADD COLUMN selected INTEGER DEFAULT 1` 迁移

### 后端 API

`internal/api/sites.go`：

- `CreateSiteRequest`：新增 `Selected bool` 字段
- `handleCreateSite`：`selected` 默认为 `true`
- `handleUpdateSite`：支持更新 `selected` 字段

`internal/api/server.go`：

- 新增 `PUT /api/sites/:id/selected` 端点（仅更新 selected 字段，轻量级）

`cmd/server/main.go`：

- 重构启动流程：将 `createDirectories` 拆分为 `createDirs`（仅创建目录）和 `ensureConfig`（处理配置文件）
- `createDirs`：在 `NewDB` 之前调用
- `ensureConfig`：在 `NewDB` 之后调用，检查 SQLite 是否有数据并决定是否写入 embed 默认配置

### 配置文件

`internal/config/config.go`：

- `SiteConfig`：新增 `Selected bool` 字段
- `ToModels`：转换时包含 `Selected`
- `Save` / `Load`：自动处理新字段

### 前端类型

`web/src/types/index.ts`：

```typescript
interface Site {
  id: string
  name: string
  url: string
  engine_type: string
  selectors: string
  enabled: boolean
  selected?: boolean  // 新增
  format_prompt?: string
  created_at?: string
}
```

### 前端 UI

`web/src/hooks/useSites.ts`：

- `fetchSites`：初始化 `selectedSites` 从 `selected=true` 读取
- `toggleSite`：调用 `PUT /api/sites/:id/selected` 持久化选择

`web/src/components/SiteSidebar.tsx`：

- checkbox 选中状态：从 `site.selected` 读取
- checkbox 点击：调用 `toggleSite` 更新后端

## 数据迁移

### 现有数据

- `selected` 列默认值为 `1`（true）
- 现有站点自动获得 `selected=true`
- 用户行为无变化（默认全选）

### 新增站点

- `selected` 默认为 `true`
- 用户主动添加说明想用

## 边缘情况

1. **用户删除所有站点后重启**：
   - SQLite 空 + YAML 存在 → 不触发 embed 写入
   - 用户意图是清空站点，不应恢复默认配置

2. **用户手动编辑 YAML 删除 selected 字段**：
   - `config.Load` 时 `Selected` 为零值 `false`
   - `SyncSites` 时会写入 `selected=0`
   - 用户需要重新勾选

3. **旧版本 exe + 新版本 YAML**：
   - 新字段被忽略，不影响旧版本运行
   - 升级后自动支持

## 验证要点

1. 首次运行：7 个预设站点全部 `enabled=true, selected=true`
2. 用户取消勾选 Qwen → `selected=false` → 重启后仍然取消
3. 用户删除 configs/ 目录 → 重启 → 站点配置保留（从 SQLite 恢复 YAML）
4. 用户删除所有站点 → 重启 → 不会恢复默认站点
5. 新增站点（选择器不完整）→ `enabled=false, selected=true` → checkbox 禁用
6. 新增站点（选择器完整）→ `enabled=true, selected=true` → checkbox 默认勾选
