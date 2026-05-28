package main

import (
	"log"
	"os"
	"path/filepath"

	"minihub/backend/internal/app"
)

func main() {
	dataDir := env("MINIHUB_DATA", filepath.Join("data"))
	err := app.RunSSHServer(app.SSHConfig{
		Addr:    env("MINIHUB_SSH_ADDR", ":2222"),
		DataDir: dataDir,
		DBPath:  env("MINIHUB_DB", filepath.Join(dataDir, "minihub.db")),
		KeyPath: env("MINIHUB_SSH_HOST_KEY", filepath.Join(dataDir, "ssh_host_key")),
	})
	if err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
