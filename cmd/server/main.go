package main

import (
	"chat-aggregator/internal/api"
	"chat-aggregator/internal/config"
	"chat-aggregator/internal/engine"
	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"
	"chat-aggregator/internal/systrayapp"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	// 1. Create necessary directories
	createDirs()

	// 2. Initialize database
	db, err := storage.NewDB("data/knowledgeclip.db")
	if err != nil {
		log.Fatalf("init db: %v", err)
	}

	// 3. Ensure config file exists (check SQLite first, restore from database if needed)
	ensureConfig(db)

	// 4. Load config
	cfg, err := config.Load("configs/sites.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 5. Sync sites to database
	sites := cfg.ToModels()
	if err := storage.SyncSites(db, sites); err != nil {
		log.Fatalf("sync sites: %v", err)
	}

	// 6. Create engine manager
	manager := engine.NewManager(db)

	// 7. Create server
	server := api.NewServer(db, manager)

	// 8. Start server in goroutine
	serverReady := make(chan int, 1)
	go func() {
		port, err := server.Run(8080)
		if err != nil {
			log.Printf("server error: %v", err)
		}
		serverReady <- port
	}()

	// 9. Wait briefly for port detection to complete
	// server.Run() checks port availability before starting, so we poll
	var actualPort int
	for i := 0; i < 20; i++ {
		if server.Port() != 0 {
			actualPort = server.Port()
			break
		}
		// Brief sleep to allow port detection
		select {
		case port := <-serverReady:
			actualPort = port
			break
		default:
		}
	}

	// If port still 0, assume 8080 (first attempt)
	if actualPort == 0 {
		actualPort = 8080
	}

	fmt.Printf("Server starting on port %d\n", actualPort)

	// 10. Set tray port
	systrayapp.SetPort(actualPort)

	// 11. Handle Ctrl+C for development mode
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		fmt.Println("\nShutting down...")
		server.Shutdown()
	}()

	// 12. Run systray (blocks until quit)
	systrayapp.Run(func() {
		// Tray exit callback
		fmt.Println("Shutting down from tray...")
		server.Shutdown()
		manager.Close()
		db.Close()
		fmt.Println("Server closed")
	})
}

// createDirs creates necessary directories (called before NewDB)
func createDirs() {
	dirs := []string{
		"configs",
		"data",
		".browser-data",
	}

	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			err := os.MkdirAll(dir, 0755)
			if err != nil {
				log.Fatalf("create directory %s: %v", dir, err)
			}
		}
	}
}

// ensureConfig ensures config file exists (called after NewDB)
// Logic: Check SQLite first, restore YAML from database if configs/ deleted
// Only write embed default config when SQLite empty AND YAML not exists
func ensureConfig(db *storage.DB) {
	configPath := filepath.Join("configs", "sites.yaml")

	// Check if config file already exists
	if _, err := os.Stat(configPath); err == nil {
		return
	}

	// Config file not exists, check if SQLite has sites data
	hasSites, err := storage.HasSites(db)
	if err != nil {
		log.Fatalf("check sites in database: %v", err)
	}

	if hasSites {
		// SQLite has data, restore YAML from database
		sites, err := storage.GetSites(db)
		if err != nil {
			log.Fatalf("get sites from database: %v", err)
		}

		cfg := restoreConfigFromSites(sites)
		if err := config.Save(configPath, cfg); err != nil {
			log.Fatalf("restore config from database: %v", err)
		}
		fmt.Println("Restored config from database: configs/sites.yaml")
	} else {
		// SQLite empty and YAML not exists, write embed default config
		err := os.WriteFile(configPath, defaultSitesConfig, 0644)
		if err != nil {
			log.Fatalf("create default config: %v", err)
		}
		fmt.Println("Created default config with preset sites: configs/sites.yaml")
	}
}

// restoreConfigFromSites converts database Site models back to Config struct
func restoreConfigFromSites(sites []models.Site) *config.Config {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			FormatPrompt:   "",
			DefaultTimeout: 30,
			MaxConcurrent:  3,
		},
		Sites: make([]config.SiteConfig, 0, len(sites)),
	}

	for _, s := range sites {
		var selectors map[string]string
		if s.Selectors != "" {
			json.Unmarshal([]byte(s.Selectors), &selectors)
		}
		if selectors == nil {
			selectors = make(map[string]string)
		}

		siteCfg := config.SiteConfig{
			ID:       s.ID,
			Name:     s.Name,
			URL:      s.URL,
			Enabled:  s.Enabled,
			Selected: s.Selected,
			Engine: config.EngineConfig{
				Primary:   s.EngineType,
				Selectors: selectors,
			},
			FormatPrompt: s.FormatPrompt,
			CookieFile:   s.CookieFile,
		}
		cfg.Sites = append(cfg.Sites, siteCfg)
	}

	return cfg
}