# Review Package for Task 2

## Commit Range
BASE: 5163b984b7403e17c4e382cb7f6a412f9b43b3a5
HEAD: 114f79256dacc579d4ebcb50fdbec9a1fac7f641

## Commit Log
114f792 feat(models): add Selected field to Site struct

## Diff Stats

 internal/models/models.go | 1 +
 1 file changed, 1 insertion(+)

## Full Diff

diff --git a/internal/models/models.go b/internal/models/models.go
index 6032a34..b7aa2b2 100644
--- a/internal/models/models.go
+++ b/internal/models/models.go
@@ -3,20 +3,21 @@ package models
 import "time"
 
 type Site struct {
 	ID           string    `json:"id"`
 	Name         string    `json:"name"`
 	URL          string    `json:"url"`
 	EngineType   string    `json:"engine_type"`
 	Selectors    string    `json:"selectors"`
 	CookieFile   string    `json:"cookie_file"`
 	Enabled      bool      `json:"enabled"`
+	Selected     bool      `json:"selected"`
 	FormatPrompt string    `json:"format_prompt"`
 	CreatedAt    time.Time `json:"created_at"`
 }
 
 type Session struct {
 	ID        string    `json:"id"`
 	Prompt    string    `json:"prompt"`
 	CreatedAt time.Time `json:"created_at"`
 }
 
