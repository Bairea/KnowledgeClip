### Task 3: Storage Layer - Add HasSites and Update CRUD Functions

**Files:**
- Modify: `internal/storage/site_store.go`

**Interfaces:**
- Consumes: `Site.Selected bool` from Task 2
- Produces: `HasSites(db *DB) (bool, error)`, all CRUD functions handle `selected` field

**Changes required:**

1. Add `HasSites` function at end of file
2. Update `SyncSites` to include selected field
3. Update `SaveSite` to include selected field
4. Update `UpdateSite` to include selected field
5. Update `GetSites` query and scan for selected
6. Update `GetSiteByID` query and scan for selected
7. Add `UpdateSelected` function

**Report file:** Write your full report to `.superpowers/sdd/task-3-report.md`

After writing the report, return:
- Status: DONE / DONE_WITH_CONCERNS / NEEDS_CONTEXT / BLOCKED
- Commits: list of commit hashes
- Test summary: one line
- Concerns: any issues found
