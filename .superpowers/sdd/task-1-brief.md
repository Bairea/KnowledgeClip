### Task 1: Database Migration - Add selected Column

**Files:**
- Modify: `internal/storage/db.go:72-78`

**Interfaces:**
- Produces: `selected INTEGER DEFAULT 1` column in sites table

- [ ] **Step 1: Add migration to db.go**

```go
migrations := []string{
    "ALTER TABLE messages ADD COLUMN turn INTEGER NOT NULL DEFAULT 0",
    "ALTER TABLE messages ADD COLUMN prompt TEXT NOT NULL DEFAULT ''",
    "ALTER TABLE sites ADD COLUMN selected INTEGER NOT NULL DEFAULT 1",
}
```

- [ ] **Step 2: Run go build to verify syntax**

Run: `go build -o bin/server.exe cmd/server/main.go`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/storage/db.go
git commit -m "feat(storage): add selected column migration to sites table"
```
