package main

import (
	"chat-aggregator/internal/api"
	"chat-aggregator/internal/config"
	"chat-aggregator/internal/storage"
	"log"
)

func main() {
	db, err := storage.NewDB("data.db")
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	cfg, err := config.Load("configs/sites.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	sites := cfg.ToModels()
	if err := storage.SyncSites(db, sites); err != nil {
		log.Fatalf("sync sites: %v", err)
	}

	server := api.NewServer(db)
	if err := server.Run(":8080"); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
