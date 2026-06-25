package engine

import (
	"context"
	"chat-aggregator/internal/models"
)

type BrowserEngine interface {
	SendMessage(ctx context.Context, site models.Site, prompt string) (string, error)
	Close() error
}

type Manager struct {
	engine BrowserEngine
}

func NewManager() *Manager {
	return &Manager{
		engine: NewRodEngine(),
	}
}

func (m *Manager) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	return m.engine.SendMessage(ctx, site, prompt)
}

func (m *Manager) Close() error {
	return m.engine.Close()
}
