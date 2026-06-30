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

func DeleteSession(db *DB, id string) error {
	tx, err := db.Conn().Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, id); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}

	result, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("session not found")
	}

	return tx.Commit()
}

func GetSessions(db *DB) ([]models.Session, error) {
	rows, err := db.Conn().Query(`SELECT id, prompt, created_at FROM sessions ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []models.Session = []models.Session{}
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
