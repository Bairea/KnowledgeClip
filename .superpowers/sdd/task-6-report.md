# Task 6 Report: Startup Logic - Split createDirectories

## Summary

Successfully implemented the fix for the data loss issue when `configs/` directory is deleted.

## Changes Made

### File: `cmd/server/main.go`

1. **Import additions**:
   - Added `encoding/json` for parsing selectors JSON from database
   - Added `internal/models` for Site type

2. **Function split**:
   - `createDirectories()` renamed to `createDirs()` - only creates directories (configs, data, .browser-data)
   - New `ensureConfig(db)` function - handles config file logic after database is initialized

3. **New function `restoreConfigFromSites(sites)`**:
   - Converts database Site models back to Config struct
   - Parses selectors JSON string back to map[string]string
   - Preserves all site properties: ID, Name, URL, Enabled, Selected, Engine, FormatPrompt, CookieFile

4. **Startup flow updated**:
   ```
   1. createDirs()        -> create directories only
   2. NewDB()             -> initialize SQLite
   3. ensureConfig(db)    -> check SQLite, restore YAML if needed
   4. Load config         -> load YAML
   5. SyncSites           -> sync to database
   ...
   ```

## Logic Flow for `ensureConfig`

```
ensureConfig(db)
  |
  +-- configs/sites.yaml exists? -> YES: return (nothing to do)
  |
  +-- NO: check SQLite via HasSites(db)
       |
       +-- SQLite has sites? -> YES: restore YAML from database
       |    - GetSites(db) -> restoreConfigFromSites() -> config.Save()
       |    - Print "Restored config from database"
       |
       +-- NO (SQLite empty): write embed defaultSitesConfig
            - Print "Created default config with preset sites"
```

## Key Design Decisions

1. **SQLite is the authoritative source**: When `configs/` directory is deleted, data is restored from SQLite, not overwritten with embed defaults.

2. **Bidirectional sync preserved**: After restore, the existing `SyncSites()` call in main() will ensure consistency between YAML and database.

3. **Selectors JSON parsing**: The database stores selectors as JSON string, restored back to map structure for YAML.

4. **First-run behavior unchanged**: When both SQLite empty and YAML not exists, embed default config is written (out-of-box experience).

## Testing

- Build successful: `go build -o bin/server.exe ./cmd/server`
- No runtime tests executed (no test suite in project)

## Edge Cases Covered

1. **configs/ deleted, SQLite has data**: YAML restored from database
2. **configs/ deleted, SQLite empty**: Embed default config written
3. **configs/ exists**: No action taken
4. **Malformed selectors JSON**: Fallback to empty map (not nil)

## No Concerns

All requirements from task brief satisfied.