# Task 1 Report: Database Migration - Add selected Column

## What Changed

**File:** `internal/storage/db.go` (line 74)

Added one line to the existing `migrations` slice:

```go
"ALTER TABLE sites ADD COLUMN selected INTEGER NOT NULL DEFAULT 1",
```

This follows the exact same pattern as the existing `turn` and `prompt` column migrations. The `selected` column is `INTEGER NOT NULL DEFAULT 1`, meaning all existing and new sites default to selected=true.

## Build Verification

```
$ cd D:/cs_proj/KnowledgeClip && go build -o bin/server.exe ./cmd/server/
# (no output - build succeeded)
```

Note: Building with `go build -o bin/server.exe cmd/server/main.go` fails due to a pre-existing issue where `//go:embed` directives in `embed_config.go` are not resolved when using file-path mode instead of package mode. This is unrelated to this task.

## Commit Hash

`5163b98`

## Concerns

None. The migration is additive-only (ALTER TABLE ADD COLUMN), which is safe for existing databases. SQLite will apply the migration idempotently -- if the column already exists, `conn.Exec` will return an error that is silently ignored (same as the existing `turn`/`prompt` migrations).
