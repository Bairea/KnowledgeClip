# Review Package for Task 1

## Commit Range
BASE: e24f3a37cce1921c9438eb0773ad8a9298f56045
HEAD: 5163b984b7403e17c4e382cb7f6a412f9b43b3a5

## Commit Log
5163b98 feat(storage): add selected column migration to sites table

## Diff Stats

 internal/storage/db.go | 1 +
 1 file changed, 1 insertion(+)

## Full Diff

diff --git a/internal/storage/db.go b/internal/storage/db.go
index e485e0a..fdabb4a 100644
--- a/internal/storage/db.go
+++ b/internal/storage/db.go
@@ -64,20 +64,21 @@ CREATE TABLE IF NOT EXISTS site_cookies (
 	FOREIGN KEY (site_id) REFERENCES sites(id)
 );
 `
 	if _, err := conn.Exec(schema); err != nil {
 		return nil, fmt.Errorf("run schema: %w", err)
 	}
 
 	migrations := []string{
 		"ALTER TABLE messages ADD COLUMN turn INTEGER NOT NULL DEFAULT 0",
 		"ALTER TABLE messages ADD COLUMN prompt TEXT NOT NULL DEFAULT ''",
+		"ALTER TABLE sites ADD COLUMN selected INTEGER NOT NULL DEFAULT 1",
 	}
 	for _, m := range migrations {
 		conn.Exec(m)
 	}
 
 	return &DB{db: conn}, nil
 }
 
 func (d *DB) Close() error {
 	return d.db.Close()
