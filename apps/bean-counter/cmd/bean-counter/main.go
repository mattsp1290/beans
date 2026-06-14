package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mattsp1290/bean-counter/internal/config"
	"github.com/mattsp1290/bean-counter/internal/server"
	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	adapter, err := appstore.NewAdapter(ctx, appstore.AdapterConfig{
		Store:          cfg.Store,
		ProjectPrefix:  cfg.ProjectPrefix,
		TerminalStates: []appstore.IssueState{"closed", "done"},
		ActiveStates:   []appstore.IssueState{"open"},
	})
	if err != nil {
		return err
	}
	defer adapter.Close()

	app := server.New(server.Config{
		CORSOrigin:    cfg.CORSOrigin,
		CORSOriginSet: true,
	})
	log.Printf("bean-counter listening on %s", cfg.Addr)
	if err := server.Run(ctx, app, cfg.Addr); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}
