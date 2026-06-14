package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mattsp1290/bean-counter/internal/config"
	"github.com/mattsp1290/bean-counter/internal/handlers/deps"
	"github.com/mattsp1290/bean-counter/internal/handlers/graph"
	"github.com/mattsp1290/bean-counter/internal/handlers/issues"
	"github.com/mattsp1290/bean-counter/internal/handlers/ready"
	"github.com/mattsp1290/bean-counter/internal/server"
	appstore "github.com/mattsp1290/bean-counter/internal/store"

	"github.com/gofiber/fiber/v3"
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
		RegisterAPI: func(api fiber.Router) {
			issues.Register(api, issues.Config{
				Store:         adapter.Store(),
				ProjectPrefix: cfg.ProjectPrefix,
				Actor:         cfg.Actor,
			})
			deps.Register(api, deps.Config{
				Store:         adapter.Store(),
				ProjectPrefix: cfg.ProjectPrefix,
			})
			ready.Register(api, ready.Config{
				Source: adapter,
			})
			graph.Register(api, graph.Config{
				Store:         adapter.Store(),
				ProjectPrefix: cfg.ProjectPrefix,
			})
		},
	})
	log.Printf("bean-counter listening on %s", cfg.Addr)
	if err := server.Run(ctx, app, cfg.Addr); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}
