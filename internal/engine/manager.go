package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

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

// BatchSender can send a prompt to multiple sites through a coordinated pipeline.
// Engines with a single-resource constraint (e.g. browser-act's single active tab)
// implement this to avoid global-lock contention from concurrent per-site calls.
type BatchSender interface {
	SendBatch(ctx context.Context, sites []models.Site, prompt string, isNewSession bool, onResult func(site models.Site, content string, err error))
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

	// Primary: rod engine (fastest, supports cookie persistence)
	re := NewRodEngine(db)
	engines = append(engines, re)

	// Fallback 1: browser-act engine (requires browser-act CLI installed)
	scriptsDir := getScriptsDir()
	baEngine, err := NewBrowserActEngine(scriptsDir)
	if err != nil {
		log.Printf("engine init: browser-act unavailable: %v", err)
		log.Printf("engine init: sites with engine=browser-act will fall back to other engines")
	} else {
		engines = append(engines, baEngine)
	}

	// Optional extension: bsk engine (browser-skill CLI, drives the user's
	// real Chromium). Never packaged, never part of the default fallback
	// chain — only sites explicitly configured with engine=bsk use it.
	bskEng, err := NewBskEngine(getScriptsDir())
	if err != nil {
		log.Printf("engine init: bsk unavailable: %v", err)
	} else {
		engines = append(engines, bskEng)
	}

	// Fallback 2: playwright-go engine
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

// exclusiveEngines are opt-in extension engines: only sites explicitly
// configured with that engine type use them, and they never serve as
// fallbacks for other sites.
var exclusiveEngines = map[string]bool{
	"browser-act": true,
	"bsk":         true,
}

// EngineHealth is the user-facing availability of one engine slot.
type EngineHealth struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

// knownEngineNames lists every engine slot in priority order, including
// ones whose construction failed (reported as unavailable).
var knownEngineNames = []string{"rod", "browser-act", "bsk", "playwright-go"}

// Health reports per-engine availability. The bsk slot gets a live
// daemon/extension probe; the rest reflect construction-time availability.
func (m *Manager) Health() []EngineHealth {
	out := make([]EngineHealth, 0, len(knownEngineNames))
	for _, name := range knownEngineNames {
		eng := m.engineByName(name)
		h := EngineHealth{Name: name}
		if eng == nil {
			h.Detail = "未安装"
		} else {
			h.Available = true
		}
		if bsk, ok := eng.(*BskEngine); ok {
			h.Available, h.Detail = bsk.Health(2 * time.Second)
		}
		out = append(out, h)
	}
	return out
}

func (m *Manager) siteLock(siteID string) *sync.Mutex {
	v, _ := m.locks.LoadOrStore(siteID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// engineByName returns the registered engine with the given name, or nil.
func (m *Manager) engineByName(name string) BrowserEngine {
	for _, eng := range m.engines {
		if eng.Name() == name {
			return eng
		}
	}
	return nil
}

func (m *Manager) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	lock := m.siteLock(site.ID)
	lock.Lock()
	defer lock.Unlock()

	// Exclusive opt-in engines: exactly the configured engine, no fallback.
	if exclusiveEngines[site.EngineType] {
		eng := m.engineByName(site.EngineType)
		if eng == nil {
			return "", fmt.Errorf("%s engine not available", site.EngineType)
		}
		result, err := eng.SendMessage(ctx, site, prompt)
		if err != nil {
			return "", fmt.Errorf("%s: %w", eng.Name(), err)
		}
		return result, nil
	}

	var errs []error
	for _, eng := range m.engines {
		// Opt-in extension engines are never tried as fallbacks.
		if exclusiveEngines[eng.Name()] {
			continue
		}
		result, err := eng.SendMessage(ctx, site, prompt)
		if err == nil {
			return result, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", eng.Name(), err))
	}

	return "", fmt.Errorf("all engines failed: %w", errors.Join(errs...))
}

// SiteProgressFunc receives per-site stage transitions during a batch send.
// elapsedMs is measured from the batch's per-site start.
type SiteProgressFunc func(siteID, stage string, elapsedMs int)

// SendToSites sends a prompt to multiple sites, calling onResult for each site
// as it completes and onProgress for stage transitions (may be nil).
// Browser-act sites are routed to the batch coordinator (single-active-tab
// constraint); other-engine sites are sent concurrently — the bsk engine
// polls its per-site tabs over the daemon socket in parallel.
func (m *Manager) SendToSites(ctx context.Context, sites []models.Site, prompt string, isNewSession bool, onResult func(site models.Site, content string, err error), onProgress SiteProgressFunc) {
	var wg sync.WaitGroup

	var baSites, otherSites []models.Site
	for _, site := range sites {
		if site.EngineType == "browser-act" {
			baSites = append(baSites, site)
		} else {
			otherSites = append(otherSites, site)
		}
	}

	// Browser-act sites: use batch coordinator (single goroutine, round-robin).
	// The coordinator fans out internally with a shared timeline, so only the
	// initial stage is reported per site here.
	if len(baSites) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if onProgress != nil {
				for _, site := range baSites {
					onProgress(site.ID, ProgressInput, 0)
				}
			}
			for _, eng := range m.engines {
				if eng.Name() != "browser-act" {
					continue
				}
				if bs, ok := eng.(BatchSender); ok {
					bs.SendBatch(ctx, baSites, prompt, isNewSession, onResult)
				}
				break
			}
		}()
	}

	// Other-engine sites: concurrent per-site (each engine manages its own pages)
	for _, site := range otherSites {
		wg.Add(1)
		go func(site models.Site) {
			defer wg.Done()
			start := time.Now()
			pctx := WithProgress(ctx, func(stage string) {
				if onProgress != nil {
					onProgress(site.ID, stage, int(time.Since(start).Milliseconds()))
				}
			})
			if isNewSession {
				m.StartNewChat([]models.Site{site})
			}
			actualPrompt := prompt
			if site.FormatPrompt != "" {
				actualPrompt = prompt + "\n\n" + site.FormatPrompt
			}
			content, err := m.SendMessage(pctx, site, actualPrompt)
			onResult(site, content, err)
		}(site)
	}

	wg.Wait()
}

func (m *Manager) StartNewChat(sites []models.Site) {
	var wg sync.WaitGroup
	for _, site := range sites {
		wg.Add(1)
		go func(site models.Site) {
			defer wg.Done()
			if exclusiveEngines[site.EngineType] {
				if eng := m.engineByName(site.EngineType); eng != nil {
					if ncs, ok := eng.(NewChatStarter); ok {
						if err := ncs.StartNewChat(site); err != nil {
							log.Printf("[manager] start new chat failed for site %s: %v", site.ID, err)
						}
					}
				}
				return
			}
			for _, eng := range m.engines {
				if exclusiveEngines[eng.Name()] {
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
