package main

import (
	"log"
	"msim/config"
	"msim/db"
	"msim/server"
	"time"
)

func main() {
	cfg := config.Load()

	database, err := db.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	srvConfig := &server.ServerConfig{
		Port:         cfg.Port,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
	}

	srv := server.New(database, srvConfig)

	log.Fatal(srv.Start())
}

