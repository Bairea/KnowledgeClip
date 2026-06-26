package storage

import (
	"fmt"

	"chat-aggregator/internal/models"
)

func UpdateMessageKept(db *DB, id string, kept bool) error {
	_, err := db.Conn().Exec(`UPDATE messages SET kept = ? WHERE id = ?`, kept, id)
	if err != nil {
		return fmt.Errorf("update message kept: %w", err)
	}
	return nil
}

func CreateMessage(db *DB, id, sessionID, siteID, content, errStr string, elapsedMs int, turn int, prompt string) error {
	_, err := db.Conn().Exec(
		`INSERT INTO messages (id, session_id, site_id, content, kept, error, elapsed_ms, turn, prompt, created_at) VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		id, sessionID, siteID, content, errStr, elapsedMs, turn, prompt,
	)
	if err != nil {
		return fmt.Errorf("create message: %w", err)
	}
	return nil
}

func GetMessagesBySession(db *DB, sessionID string) ([]models.Message, error) {
	rows, err := db.Conn().Query(
		`SELECT id, session_id, site_id, content, kept, error, elapsed_ms, turn, prompt, created_at FROM messages WHERE session_id = ? ORDER BY turn, created_at`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query messages by session: %w", err)
	}
	defer rows.Close()

	var messages []models.Message = []models.Message{}
	for rows.Next() {
		var msg models.Message
		var kept int
		err := rows.Scan(&msg.ID, &msg.SessionID, &msg.SiteID, &msg.Content, &kept, &msg.Error, &msg.ElapsedMs, &msg.Turn, &msg.Prompt, &msg.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msg.Kept = kept != 0
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}

	return messages, nil
}
