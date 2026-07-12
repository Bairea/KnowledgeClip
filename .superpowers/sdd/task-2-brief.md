### Task 2: Model - Add Selected Field to Site Struct

**Files:**
- Modify: `internal/models/models.go:5-15`

**Interfaces:**
- Produces: `Site.Selected bool` field with json tag

- [ ] **Step 1: Add Selected field to Site struct**

```go
type Site struct {
    ID           string    `json:"id"`
    Name         string    `json:"name"`
    URL          string    `json:"url"`
    EngineType   string    `json:"engine_type"`
    Selectors    string    `json:"selectors"`
    CookieFile   string    `json:"cookie_file"`
    Enabled      bool      `json:"enabled"`
    Selected     bool      `json:"selected"`
    FormatPrompt string    `json:"format_prompt"`
    CreatedAt    time.Time `json:"created_at"`
}
```

- [ ] **Step 2: Run go build to verify syntax**

Run: `go build -o bin/server.exe cmd/server/main.go`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/models/models.go
git commit -m "feat(models): add Selected field to Site struct"
```
