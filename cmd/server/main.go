package main

import (
	"chat-aggregator/internal/api"
	"chat-aggregator/internal/config"
	"chat-aggregator/internal/engine"
	"chat-aggregator/internal/storage"
	"chat-aggregator/internal/systrayapp"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	// 1. Create necessary directories
	createDirectories()

	// 2. Initialize database
	db, err := storage.NewDB("data/knowledgeclip.db")
	if err != nil {
		log.Fatalf("init db: %v", err)
	}

	// 3. Load config
	cfg, err := config.Load("configs/sites.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 4. Sync sites to database
	sites := cfg.ToModels()
	if err := storage.SyncSites(db, sites); err != nil {
		log.Fatalf("sync sites: %v", err)
	}

	// 5. Create engine manager
	manager := engine.NewManager(db)

	// 6. Create server
	server := api.NewServer(db, manager)

	// 7. Start server in goroutine
	serverReady := make(chan int, 1)
	go func() {
		port, err := server.Run(8080)
		if err != nil {
			log.Printf("server error: %v", err)
		}
		serverReady <- port
	}()

	// 8. Wait briefly for port detection to complete
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

	// 9. Set tray port
	systrayapp.SetPort(actualPort)

	// 10. Handle Ctrl+C for development mode
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		fmt.Println("\nShutting down...")
		server.Shutdown()
	}()

	// 11. Run systray (blocks until quit)
	systrayapp.Run(func() {
		// Tray exit callback
		fmt.Println("Shutting down from tray...")
		server.Shutdown()
		manager.Close()
		db.Close()
		fmt.Println("Server closed")
	})
}

// createDirectories creates necessary directories
func createDirectories() {
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

	// Create default config if not exists
	configPath := filepath.Join("configs", "sites.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := `sites: []
`
		err := os.WriteFile(configPath, []byte(defaultConfig), 0644)
		if err != nil {
			log.Fatalf("create default config: %v", err)
		}
		fmt.Println("Created default config: configs/sites.yaml")
	}
}