package engine

import (
	"context"
	"errors"
	"fmt"

	"chat-aggregator/internal/models"
)

type BrowserEngine interface {
	SendMessage(ctx context.Context, site models.Site, prompt string) (string, error)
	Close() error
}

type Manager struct {
	engines []BrowserEngine
}

func NewManager() *Manager {
	return &Manager{
		engines: getEngines(),
	}
}

func getEngines() []BrowserEngine {
	var engines []BrowserEngine
	var errs []error

	re := NewRodEngine()
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
