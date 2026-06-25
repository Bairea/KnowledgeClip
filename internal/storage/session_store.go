package storage

import "fmt"

func CreateSession(db *DB, id string, prompt string) error {
	_, err := db.Conn().Exec(`INSERT INTO sessions (id, prompt) VALUES (?, ?)`, id, prompt)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func CreateMessage(db *DB, id string, sessionID string, siteID string, content string, errorStr string, elapsedMs int) error {
	_, err := db.Conn().Exec(
		`INSERT INTO messages (id, session_id, site_id, content, error, elapsed_ms) VALUES (?, ?, ?, ?, ?, ?)`,
		id, sessionID, siteID, content, errorStr, elapsedMs,
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}
