# Task 5 Report: API Layer - Add handleUpdateSelected

## Status: DONE

## Changes Made

### 1. Added `UpdateSelectedRequest` struct (internal/api/sites.go)
```go
type UpdateSelectedRequest struct {
	Selected bool `json:"selected"`
}
```

### 2. Added `handleUpdateSelected` function (internal/api/sites.go)
- Endpoint handler for `PUT /api/sites/:id/selected`
- Binds JSON request body to `UpdateSelectedRequest`
- Calls `storage.UpdateSelected(db, id, selected)` to update database
- Calls `syncConfigToYAML()` to persist changes to YAML config
- Returns `{"message": "updated"}` on success

### 3. Registered route (internal/api/server.go)
```go
s.router.PUT("/api/sites/:id/selected", s.handleUpdateSelected)
```

## API Contract

**Endpoint:** `PUT /api/sites/:id/selected`

**Request Body:**
```json
{
  "selected": true
}
```

**Response (success):**
```json
{
  "message": "updated"
}
```

**Response (error):**
```json
{
  "error": "error message"
}
```

## Verification

- API package compiled successfully: `go build ./internal/api/...`
- Note: Full binary build fails due to pre-existing issue with `defaultSitesConfig` in cmd/server/main.go (unrelated to this task)

## Dependencies

- Task 3 already implemented `storage.UpdateSelected(db, id, selected) error`
- Task 3 already updated `syncConfigToYAML()` to include `Selected` field
