package engine

import (
	"context"
	"errors"
	"fmt"

	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"
)

type BrowserEngine interface {
	SendMessage(ctx context.Context, site models.Site, prompt string) (string, error)
	Close() error
}

type Manager struct {
	engines []BrowserEngine
}

func NewManager(db *storage.DB) *Manager {
	return &Manager{
		engines: getEngines(db),
	}
}

func getEngines(db *storage.DB) []BrowserEngine {
	var engines []BrowserEngine
	var errs []error

	re := NewRodEngine(db)
	engines = append(engines, re)

	pwe, err := NewPlaywrightGoEngine()
	if err != nil {
		errs = append(errs, fmt.Errorf("playwright-go: %w", err))
	} else {
		engines = append(engines, pwe)
	}

	ts, err := NewTSPlaywrightEngine()
	if err != nil {
		errs = append(errs, fmt.Errorf("ts-playwright: %w", err))
	} else {
		engines = append(engines, ts)
	}

	_ = errs
	return engines
}

func (m *Manager) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	var errs []error

	for _, eng := range m.engines {
		result, err := eng.SendMessage(ctx, site, prompt)
		if err == nil {
			return result, nil
		}
		errs = append(errs, err)
	}

	return "", fmt.Errorf("all engines failed: %w", errors.Join(errs...))
}

func (m *Manager) Close() error {
	var errs []error
	for _, eng := range m.engines {
		if err := eng.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
