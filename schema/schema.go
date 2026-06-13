package schema

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

// migrateAdvisoryLockID is a stable int64 for pg_advisory_lock.
const migrateAdvisoryLockID int64 = 0x626e74726b72 // "bntrkr"

var migrationFilenameRE = regexp.MustCompile(`^([0-9]{4,})_([A-Za-z0-9][A-Za-z0-9_\-]*)\.sql$`)

// Migration is one parsed entry from the embedded migrations directory.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrations returns the embedded migration filesystem.
func Migrations() embed.FS {
	return migrationsFS
}

// ListMigrations returns every embedded migration sorted ascending by Version.
func ListMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationsFS, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("schema: read migrations dir: %w", err)
	}

	out := make([]Migration, 0, len(entries))
	seen := make(map[int]string, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		matches := migrationFilenameRE.FindStringSubmatch(name)
		if matches == nil {
			return nil, fmt.Errorf("schema: migration %q does not match NNNN_name.sql", name)
		}
		version, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, fmt.Errorf("schema: parse version in %q: %w", name, err)
		}
		if existing, ok := seen[version]; ok {
			return nil, fmt.Errorf("schema: duplicate migration version %d (%q and %q)", version, existing, name)
		}
		seen[version] = name

		body, err := fs.ReadFile(migrationsFS, migrationsDir+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("schema: read %q: %w", name, err)
		}
		out = append(out, Migration{
			Version: version,
			Name:    matches[2],
			SQL:     string(body),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// Migrate applies all embedded bn migrations using goose's library API.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("schema: migrate: nil pool")
	}

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	migrations, err := fs.Sub(migrationsFS, migrationsDir)
	if err != nil {
		return fmt.Errorf("schema: migrations fs: %w", err)
	}

	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(migrateAdvisoryLockID))
	if err != nil {
		return fmt.Errorf("schema: postgres session locker: %w", err)
	}

	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations,
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		return fmt.Errorf("schema: goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("schema: migrate up: %w", err)
	}
	return nil
}
