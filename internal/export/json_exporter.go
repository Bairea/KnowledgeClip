package export

import (
	"encoding/json"
	"fmt"

	"chat-aggregator/internal/models"
)

type SessionExport struct {
	Session  models.Session   `json:"session"`
	Messages []models.Message `json:"messages"`
}

func ToJSON(session models.Session, messages []models.Message) ([]byte, error) {
	export := SessionExport{
		Session:  session,
		Messages: messages,
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal export to JSON: %w", err)
	}

	return data, nil
}
