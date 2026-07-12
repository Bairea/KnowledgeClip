### Task 6: Startup Logic - Split createDirectories and Check SQLite

**Files:**
- Modify: `cmd/server/main.go`

**What needs to be done:**

1. Split `createDirectories` into:
   - `createDirs()` - only creates directories (called before NewDB)
   - `ensureConfig(db)` - handles config file logic (called after NewDB)

2. `ensureConfig` logic:
   - Check if SQLite has sites data via `storage.HasSites(db)`
   - If SQLite has data: restore YAML from database
   - If SQLite empty and YAML not exists: write embed default config

3. Update `main()` flow:
   - Call `createDirs()` first
   - Then `NewDB()`
   - Then `ensureConfig(db)`

4. Add `encoding/json` import for YAML restore logic

**Global constraints:**
- Check SQLite first, restore YAML from database if configs/ deleted
- Only write embed default config when SQLite empty AND YAML not exists

**Report file:** Write to `.superpowers/sdd/task-6-report.md`
