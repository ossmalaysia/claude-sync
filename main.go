package main

import (
	"context"
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/ossmalaysia/claude-sync/internal/app"
	"github.com/ossmalaysia/claude-sync/internal/browser"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	root, err := store.DefaultRoot()
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(root)
	if err != nil {
		log.Fatal(err)
	}
	// Return a nil interface on error: a nil *browser.Session wrapped in
	// app.Session would compare non-nil.
	open := func(profileDir string) (app.Session, error) {
		exe, err := browser.FindBrowser()
		if err != nil {
			return nil, err
		}
		s, err := browser.Open(exe, profileDir)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	emit := func(ctx context.Context, name string, data any) { runtime.EventsEmit(ctx, name, data) }
	a := app.New(st, open, emit)

	err = wails.Run(&options.App{
		Title:            "Claude Sync",
		Width:            780,
		Height:           660,
		MinWidth:         640,
		MinHeight:        560,
		BackgroundColour: &options.RGBA{R: 244, G: 246, B: 243, A: 255}, // --paper, avoids a white flash
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        a.Startup,
		OnShutdown:       a.Shutdown,
		Bind:             []interface{}{a},
	})
	if err != nil {
		log.Fatal(err)
	}
}
