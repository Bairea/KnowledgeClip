package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"
	"chat-aggregator/scripts/browser-act"
)

type BrowserEngine interface {
	SendMessage(ctx context.Context, site models.Site, prompt string) (string, error)
	Close() error
	Name() string
}

type PageResetter interface {
	ResetPages()
}

type NewChatStarter interface {
	StartNewChat(site models.Site) error
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

	// Primary: browser-act engine (requires browser-act CLI installed)
	scriptsDir := getScriptsDir()
	baEngine, err := NewBrowserActEngine(scriptsDir)
	if err != nil {
		log.Printf("engine init: browser-act unavailable: %v", err)
		log.Printf("engine init: sites with engine=browser-act will fall back to other engines")
	} else {
		engines = append(engines, baEngine)
	}

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

// getScriptsDir returns the directory containing browser-act JS snippets.
// Priority: env var > embedded resources > exe-relative path
func getScriptsDir() string {
	// 1. Environment variable override
	if dir := os.Getenv("BROWSER_ACT_SCRIPTS_DIR"); dir != "" {
		return dir
	}

	// 2. Try embedded resources (extracted to temp dir)
	if dir, err := browseract.ExtractTo(); err == nil && dir != "" {
		log.Printf("[browser-act] using embedded scripts: %s", dir)
		return dir
	}

	// 3. Fallback: resolve relative to executable location
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("Warning: could not get executable path: %v, using relative path", err)
		return "scripts/browser-act"
	}
	exeDir := filepath.Dir(exePath)
	return filepath.Join(exeDir, "scripts", "browser-act")
}

// UseBrowserAct returns true if the engine for the given site should use browser-act.
// This is determined by the site's engine type in config.
func (m *Manager) UseBrowserAct(site models.Site) bool {
	return site.EngineType == "browser-act"
}

func (m *Manager) siteLock(siteID string) *sync.Mutex {
	v, _ := m.locks.LoadOrStore(siteID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (m *Manager) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	lock := m.siteLock(site.ID)
	lock.Lock()
	defer lock.Unlock()

	// If site uses browser-act, only try browser-act engine
	if site.EngineType == "browser-act" {
		for _, eng := range m.engines {
			if eng.Name() == "browser-act" {
				result, err := eng.SendMessage(ctx, site, prompt)
				if err == nil {
					return result, nil
				}
				return "", fmt.Errorf("browser-act: %w", err)
			}
		}
		return "", errors.New("browser-act engine not available")
	}

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

func (m *Manager) StartNewChat(sites []models.Site) {
	var wg sync.WaitGroup
	for _, site := range sites {
		wg.Add(1)
		go func(site models.Site) {
			defer wg.Done()
			for _, eng := range m.engines {
				// Use browser-act for sites configured with it, otherwise use any NewChatStarter
				if site.EngineType == "browser-act" && eng.Name() != "browser-act" {
					continue
				}
				if ncs, ok := eng.(NewChatStarter); ok {
					if err := ncs.StartNewChat(site); err != nil {
						log.Printf("[manager] start new chat failed for site %s: %v", site.ID, err)
					}
					break
				}
			}
		}(site)
	}
	wg.Wait()
	log.Printf("[manager] start new chat completed for %d sites", len(sites))
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
