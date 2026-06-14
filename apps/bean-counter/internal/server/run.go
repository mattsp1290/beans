package server

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
)

const DefaultShutdownTimeout = 10 * time.Second

type RunConfig struct {
	ShutdownTimeout time.Duration
}

// Run starts app on addr and shuts it down when ctx is canceled.
func Run(ctx context.Context, app *fiber.App, addr string, config ...RunConfig) error {
	cfg := runConfigWithDefaults(config...)
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Listen(addr)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		if err := app.ShutdownWithTimeout(cfg.ShutdownTimeout); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		err := <-errCh
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}

func runConfigWithDefaults(config ...RunConfig) RunConfig {
	cfg := RunConfig{ShutdownTimeout: DefaultShutdownTimeout}
	if len(config) == 0 {
		return cfg
	}
	if config[0].ShutdownTimeout > 0 {
		cfg.ShutdownTimeout = config[0].ShutdownTimeout
	}
	return cfg
}
