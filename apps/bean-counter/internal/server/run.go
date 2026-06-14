package server

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v3"
)

// Run starts app on addr and shuts it down when ctx is canceled.
func Run(ctx context.Context, app *fiber.App, addr string) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Listen(addr)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		if err := app.Shutdown(); err != nil {
			return err
		}
		err := <-errCh
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}
