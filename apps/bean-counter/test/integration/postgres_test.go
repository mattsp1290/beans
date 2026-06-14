//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

const (
	postgresPrefix = "postgres-it"
	postgresActor  = "postgres-integration-test"
)

func TestPostgresCRUDDepsAndReadyOverHTTP(t *testing.T) {
	app, closeStore := newPostgresApp(t)
	defer closeStore()

	exerciseCRUDDepsReady(t, app, "postgres")
}

func newPostgresApp(t *testing.T) (*fiber.App, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	container, err := postgres.Run(
		ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("bean_counter"),
		postgres.WithUsername("bean_counter"),
		postgres.WithPassword("bean_counter"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		terminateCtx, terminateCancel := context.WithTimeout(context.Background(), time.Minute)
		defer terminateCancel()
		if err := container.Terminate(terminateCtx); err != nil {
			t.Fatalf("terminate postgres container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}

	return newIntegrationApp(t, ctx, integrationAppConfig{
		store: appstore.Config{
			Driver: appstore.DriverPostgres,
			DSN:    appstore.SecretDSN(dsn),
		},
		prefix: postgresPrefix,
		actor:  postgresActor,
	})
}
