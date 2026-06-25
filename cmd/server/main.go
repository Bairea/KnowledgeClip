package main

import (
	"chat-aggregator/internal/api"
	"chat-aggregator/internal/storage"
	"log"
)

func main() {
	db, err := storage.NewDB("data.db")
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	api.NewServer(db)
}
