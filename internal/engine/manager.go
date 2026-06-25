package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"
)

type BrowserEngine interface {
	SendMessage(ctx context.Context, site models.Site, prompt string) (string, error)
	Close() error
	Name() string
}

type PageResetter interface {
	ResetPages()
}

type Manager struct {
	engines []BrowserEngine
	locks   sync.Map
}

func NewManager(db *storage.DB) *Manager {
	return &Manager{
		engines: getEngines(db),
	}
}

func getEngines(db *storage.DB) []BrowserEngine {
	var engines []BrowserEngine

	re := NewRodEngine(db)
	engines = append(engines, re)

	pwe, err := NewPlaywrightGoEngine()
	if err != nil {
		log.Printf("engine init: playwright-go unavailable: %v", err)
	} else {
		pwe.SetDB(db)
		engines = append(engines, pwe)
	}

	return engines
}

func (m *Manager) siteLock(siteID string) *sync.Mutex {
	v, _ := m.locks.LoadOrStore(siteID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (m *Manager) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	lock := m.siteLock(site.ID)
	lock.Lock()
	defer lock.Unlock()

	var errs []error

	for _, eng := range m.engines {
		result, err := eng.SendMessage(ctx, site, prompt)
		if err == nil {
			return result, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", eng.Name(), err))
	}

	return "", fmt.Errorf("all engines failed: %w", errors.Join(errs...))
}

func (m *Manager) ResetPages() {
	for _, eng := range m.engines {
		if pr, ok := eng.(PageResetter); ok {
			pr.ResetPages()
		}
	}
}

func (m *Manager) Engines() []string {
	names := make([]string, 0, len(m.engines))
	for _, e := range m.engines {
		names = append(names, e.Name())
	}
	return names
}

func (m *Manager) Close() error {
	var errs []error
	for i := len(m.engines) - 1; i >= 0; i-- {
		if err := m.engines[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
