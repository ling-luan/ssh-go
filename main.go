package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"ssh-forwarder/internal/config"
	"ssh-forwarder/internal/tunnel"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	configPath := flag.String("config", config.DefaultConfigFile, "path to JSON config file")
	flag.Parse()

	store, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	manager := tunnel.NewManager(store)
	app := NewApp(store, manager)

	err = wails.Run(&options.App{
		Title:     "SSH Forwarder",
		Width:     1280,
		Height:    820,
		MinWidth:  1120,
		MinHeight: 680,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 238, G: 242, B: 247, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		shutdown(manager)
		log.Fatalf("run GUI: %v", err)
	}
}

func shutdown(manager *tunnel.Manager) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	manager.Shutdown(shutdownCtx)
}
