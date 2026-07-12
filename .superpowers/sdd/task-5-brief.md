### Task 5: API Layer - Add handleUpdateSelected and Update Request Struct

**Files:**
- Modify: `internal/api/sites.go`
- Modify: `internal/api/server.go`

**What needs to be done:**

1. Add `Selected bool` to `CreateSiteRequest` struct
2. Update `handleCreateSite` - set Selected=true for new sites (already done in Task 3)
3. Update `handleUpdateSite` - support updating Selected field (already done in Task 3)
4. Add `handleUpdateSelected` function for PUT /api/sites/:id/selected endpoint
5. Update `syncConfigToYAML` to include Selected (already done in Task 3)
6. Register route: `api.PUT("/sites/:id/selected", s.handleUpdateSelected)`

**Global constraints:**
- Endpoint: `PUT /api/sites/:id/selected`
- Request body: `{"selected": true|false}`
- Response: `{"message": "updated"}`

**Report file:** Write to `.superpowers/sdd/task-5-report.md`
