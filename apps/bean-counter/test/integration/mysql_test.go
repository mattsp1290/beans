//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/testcontainers/testcontainers-go/modules/mysql"

	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

const (
	mysqlPrefix = "mysql-it"
	mysqlActor  = "mysql-integration-test"
)

func TestMySQLCRUDDepsAndReadyOverHTTP(t *testing.T) {
	app, closeStore := newMySQLApp(t)
	defer closeStore()

	exerciseCRUDDepsReady(t, app, "mysql")
}

func newMySQLApp(t *testing.T) (*fiber.App, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	container, err := mysql.Run(
		ctx,
		"mysql:9.5",
		mysql.WithDatabase("bean_counter"),
		mysql.WithUsername("bean_counter"),
		mysql.WithPassword("bean_counter"),
	)
	if err != nil {
		t.Fatalf("start mysql container: %v", err)
	}
	t.Cleanup(func() {
		terminateCtx, terminateCancel := context.WithTimeout(context.Background(), time.Minute)
		defer terminateCancel()
		if err := container.Terminate(terminateCtx); err != nil {
			t.Fatalf("terminate mysql container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "parseTime=true", "loc=UTC", "multiStatements=true")
	if err != nil {
		t.Fatalf("mysql connection string: %v", err)
	}

	return newIntegrationApp(t, ctx, integrationAppConfig{
		store: appstore.Config{
			Driver: appstore.DriverMySQL,
			DSN:    appstore.SecretDSN(dsn),
		},
		prefix: mysqlPrefix,
		actor:  mysqlActor,
	})
}
