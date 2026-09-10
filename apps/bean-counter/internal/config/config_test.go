package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

func TestLoadEnvMapsDatabaseAndAppConfig(t *testing.T) {
	cfg, err := LoadEnv(mapLookup(map[string]string{
		"BN_DRIVER":          "mysql",
		"BN_DSN":             "user:secret@tcp(localhost:3306)/beans",
		"BN_MAX_CONNS":       "12",
		"BN_MIN_CONNS":       "3",
		"BN_CONNECT_TIMEOUT": "750ms",
		"BN_PROJECT_PREFIX":  "bc",
		"BN_ACTOR":           "agent",
		"BN_CORS_ORIGIN":     "http://localhost:5173",
		"BN_ADDR":            ":9090",
	}))
	if err != nil {
		t.Fatalf("LoadEnv error = %v", err)
	}
	if cfg.Store.Driver != appstore.DriverMySQL {
		t.Fatalf("driver = %q, want mysql", cfg.Store.Driver)
	}
	if cfg.Store.DSN.Reveal() != "user:secret@tcp(localhost:3306)/beans" {
		t.Fatalf("dsn not mapped")
	}
	if cfg.Store.MaxConns != 12 || cfg.Store.MinConns != 3 {
		t.Fatalf("pool settings = max %d min %d", cfg.Store.MaxConns, cfg.Store.MinConns)
	}
	if cfg.Store.ConnectTimeout != 750*time.Millisecond {
		t.Fatalf("connect timeout = %s", cfg.Store.ConnectTimeout)
	}
	if cfg.ProjectPrefix != "bc" || cfg.Actor != "agent" || cfg.CORSOrigin != "http://localhost:5173" || cfg.Addr != ":9090" {
		t.Fatalf("app config not mapped: %+v", cfg)
	}
}

func TestLoadEnvDefaultsNonSecretAppConfig(t *testing.T) {
	cfg, err := LoadEnv(mapLookup(map[string]string{
		"BN_DSN": "postgres://user:secret@localhost/beans",
	}))
	if err != nil {
		t.Fatalf("LoadEnv error = %v", err)
	}
	if cfg.Store.Driver != appstore.DriverPostgres {
		t.Fatalf("driver = %q, want postgres", cfg.Store.Driver)
	}
	if cfg.Addr != defaultAddr || cfg.ProjectPrefix != defaultProjectPrefix || cfg.Actor != defaultActor || cfg.CORSOrigin != defaultCORSOrigin {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoadEnvNormalizesSupportedDrivers(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want appstore.Driver
	}{
		{name: "postgres", raw: "postgres", want: appstore.DriverPostgres},
		{name: "mysql", raw: "mysql", want: appstore.DriverMySQL},
		{name: "sqlite", raw: "sqlite", want: appstore.DriverSQLite},
		{name: "uppercase whitespace", raw: " SQLITE ", want: appstore.DriverSQLite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadEnv(mapLookup(map[string]string{
				"BN_DRIVER": tt.raw,
				"BN_DSN":    "file::memory:",
			}))
			if err != nil {
				t.Fatalf("LoadEnv error = %v", err)
			}
			if cfg.Store.Driver != tt.want {
				t.Fatalf("driver = %q, want %q", cfg.Store.Driver, tt.want)
			}
		})
	}
}

func TestLoadEnvValidatesStoreConfig(t *testing.T) {
	_, err := LoadEnv(mapLookup(map[string]string{
		"BN_DRIVER": "sqlite",
	}))
	if !errors.Is(err, appstore.ErrEmptyDSN) {
		t.Fatalf("LoadEnv error = %v, want ErrEmptyDSN", err)
	}
}

func TestLoadEnvRejectsUnsupportedDriver(t *testing.T) {
	_, err := LoadEnv(mapLookup(map[string]string{
		"BN_DRIVER": "oracle",
		"BN_DSN":    "oracle://secret",
	}))
	if !errors.Is(err, appstore.ErrUnsupportedDriver) {
		t.Fatalf("LoadEnv error = %v, want ErrUnsupportedDriver", err)
	}
	if err != nil && strings.Contains(err.Error(), "oracle://secret") {
		t.Fatalf("error leaked DSN: %v", err)
	}
}

func TestStoreDSNFormattingIsRedacted(t *testing.T) {
	cfg, err := LoadEnv(mapLookup(map[string]string{
		"BN_DSN": "postgres://user:secret@localhost/beans",
	}))
	if err != nil {
		t.Fatalf("LoadEnv error = %v", err)
	}

	formatted := fmt.Sprintf("%v", cfg.Store)
	if strings.Contains(formatted, "secret") || strings.Contains(formatted, "postgres://") {
		t.Fatalf("formatted store config leaked DSN: %s", formatted)
	}

	encoded, err := json.Marshal(cfg.Store.DSN)
	if err != nil {
		t.Fatalf("marshal DSN: %v", err)
	}
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "postgres://") {
		t.Fatalf("encoded DSN leaked raw value: %s", encoded)
	}
}

func TestLoadEnvRejectsInvalidNumbers(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "max conns",
			env:  map[string]string{"BN_DSN": "postgres://secret", "BN_MAX_CONNS": "nope"},
			want: "BN_MAX_CONNS must be a 32-bit integer",
		},
		{
			name: "negative min conns",
			env:  map[string]string{"BN_DSN": "postgres://secret", "BN_MIN_CONNS": "-1"},
			want: "BN_MIN_CONNS must be non-negative",
		},
		{
			name: "connect timeout",
			env:  map[string]string{"BN_DSN": "postgres://secret", "BN_CONNECT_TIMEOUT": "slow"},
			want: "BN_CONNECT_TIMEOUT must be a duration",
		},
		{
			name: "negative connect timeout",
			env:  map[string]string{"BN_DSN": "postgres://secret", "BN_CONNECT_TIMEOUT": "-1s"},
			want: "BN_CONNECT_TIMEOUT must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadEnv(mapLookup(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadEnv error = %v, want %q", err, tt.want)
			}
			if strings.Contains(fmt.Sprint(err), "postgres://secret") {
				t.Fatalf("error leaked DSN: %v", err)
			}
		})
	}
}

func TestConfigValidateRequiresAppFields(t *testing.T) {
	base := Config{
		Addr:          ":8080",
		ProjectPrefix: "bc",
		Actor:         "agent",
		Store: appstore.Config{
			Driver: appstore.DriverSQLite,
			DSN:    appstore.SecretDSN("file::memory:"),
		},
	}

	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"addr", func(c *Config) { c.Addr = "" }, "BN_ADDR"},
		{"project prefix", func(c *Config) { c.ProjectPrefix = "" }, "BN_PROJECT_PREFIX"},
		{"actor", func(c *Config) { c.Actor = "" }, "BN_ACTOR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want %s", err, tt.want)
			}
		})
	}
}

func TestLoadEnvReadsDSNFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bn_dsn")
	// Trailing newline is common when a secret is written with `echo`; it must
	// be trimmed so the DSN is usable.
	if err := os.WriteFile(path, []byte("postgres://user:secret@postgres:5432/beans\n"), 0o600); err != nil {
		t.Fatalf("write dsn file: %v", err)
	}

	cfg, err := LoadEnv(mapLookup(map[string]string{
		"BN_DSN_FILE": path,
	}))
	if err != nil {
		t.Fatalf("LoadEnv error = %v", err)
	}
	if got := cfg.Store.DSN.Reveal(); got != "postgres://user:secret@postgres:5432/beans" {
		t.Fatalf("dsn = %q, want trimmed file contents", got)
	}
}

func TestLoadEnvRejectsBothDSNAndDSNFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bn_dsn")
	if err := os.WriteFile(path, []byte("postgres://from-file@postgres:5432/beans"), 0o600); err != nil {
		t.Fatalf("write dsn file: %v", err)
	}

	_, err := LoadEnv(mapLookup(map[string]string{
		"BN_DSN":      "postgres://from-env@localhost/beans",
		"BN_DSN_FILE": path,
	}))
	if err == nil || !strings.Contains(err.Error(), "only one of BN_DSN or BN_DSN_FILE") {
		t.Fatalf("LoadEnv error = %v, want both-set rejection", err)
	}
	if strings.Contains(fmt.Sprint(err), "from-env") || strings.Contains(fmt.Sprint(err), "from-file") {
		t.Fatalf("error leaked DSN: %v", err)
	}
}

func TestLoadEnvMissingDSNFileErrorsWithoutLeak(t *testing.T) {
	_, err := LoadEnv(mapLookup(map[string]string{
		"BN_DSN_FILE": filepath.Join(t.TempDir(), "does-not-exist"),
	}))
	if err == nil || !strings.Contains(err.Error(), "reading BN_DSN_FILE") {
		t.Fatalf("LoadEnv error = %v, want read failure", err)
	}
}

func TestLoadEnvEmptyDSNFileIsEmptyDSN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bn_dsn")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("write dsn file: %v", err)
	}

	_, err := LoadEnv(mapLookup(map[string]string{
		"BN_DSN_FILE": path,
	}))
	if !errors.Is(err, appstore.ErrEmptyDSN) {
		t.Fatalf("LoadEnv error = %v, want ErrEmptyDSN", err)
	}
}

func TestDSNFromFileIsRedactedInFormatting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bn_dsn")
	if err := os.WriteFile(path, []byte("postgres://user:secret@postgres:5432/beans"), 0o600); err != nil {
		t.Fatalf("write dsn file: %v", err)
	}

	cfg, err := LoadEnv(mapLookup(map[string]string{
		"BN_DSN_FILE": path,
	}))
	if err != nil {
		t.Fatalf("LoadEnv error = %v", err)
	}
	formatted := fmt.Sprintf("%v", cfg.Store)
	if strings.Contains(formatted, "secret") || strings.Contains(formatted, "postgres://") {
		t.Fatalf("formatted store config leaked file-sourced DSN: %s", formatted)
	}
}

func mapLookup(values map[string]string) LookupFunc {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
