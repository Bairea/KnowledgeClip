package storage

import (
	"database/sql"
	"fmt"

	"chat-aggregator/internal/models"
)

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

func GetSessionByID(db *DB, id string) (*models.Session, error) {
	row := db.Conn().QueryRow(`SELECT id, prompt, created_at FROM sessions WHERE id = ?`, id)
	var session models.Session
	if err := row.Scan(&session.ID, &session.Prompt, &session.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan session: %w", err)
	}
	return &session, nil
}

func GetSessions(db *DB) ([]models.Session, error) {
	rows, err := db.Conn().Query(`SELECT id, prompt, created_at FROM sessions ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []models.Session
	for rows.Next() {
		var session models.Session
		if err := rows.Scan(&session.ID, &session.Prompt, &session.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}

	return sessions, nil
}
