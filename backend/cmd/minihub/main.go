package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"minihub/backend/internal/app"
)

func main() {
	addr := env("MINIHUB_ADDR", ":8080")
	dataDir := env("MINIHUB_DATA", filepath.Join("data"))
	dbPath := env("MINIHUB_DB", filepath.Join(dataDir, "minihub.db"))
	frontendDir := env("MINIHUB_FRONTEND", filepath.Join("..", "frontend", "dist"))

	server, err := app.NewServer(app.Config{
		DataDir:     dataDir,
		DBPath:      dbPath,
		FrontendDir: frontendDir,
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("minihub listening on %s", addr)
	certFile := os.Getenv("MINIHUB_TLS_CERT")
	keyFile := os.Getenv("MINIHUB_TLS_KEY")
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			log.Fatal("MINIHUB_TLS_CERT and MINIHUB_TLS_KEY must both be set")
		}
		log.Fatal(http.ListenAndServeTLS(addr, certFile, keyFile, server))
	}
	log.Fatal(http.ListenAndServe(addr, server))
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
