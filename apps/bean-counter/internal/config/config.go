package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

const (
	defaultAddr          = ":8080"
	defaultProjectPrefix = "bean-counter"
	defaultActor         = "bean-counter"
	defaultCORSOrigin    = "http://localhost:5173"
)

type LookupFunc func(string) (string, bool)

// Config is the process-level application configuration loaded from the
// environment.
type Config struct {
	Addr          string
	ProjectPrefix string
	Actor         string
	CORSOrigin    string
	Store         appstore.Config
}

func Load() (Config, error) {
	return LoadEnv(os.LookupEnv)
}

func LoadEnv(lookup LookupFunc) (Config, error) {
	dsn, err := resolveDSN(lookup)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Addr:          getString(lookup, "BN_ADDR", defaultAddr),
		ProjectPrefix: getString(lookup, "BN_PROJECT_PREFIX", defaultProjectPrefix),
		Actor:         getString(lookup, "BN_ACTOR", defaultActor),
		CORSOrigin:    getString(lookup, "BN_CORS_ORIGIN", defaultCORSOrigin),
		Store: appstore.Config{
			Driver: appstore.DriverPostgres,
			DSN:    appstore.SecretDSN(dsn),
		},
	}

	if raw, ok := lookup("BN_DRIVER"); ok && strings.TrimSpace(raw) != "" {
		cfg.Store.Driver = appstore.Driver(strings.ToLower(strings.TrimSpace(raw)))
	}

	maxConns, err := getInt32(lookup, "BN_MAX_CONNS")
	if err != nil {
		return Config{}, err
	}
	cfg.Store.MaxConns = maxConns

	minConns, err := getInt32(lookup, "BN_MIN_CONNS")
	if err != nil {
		return Config{}, err
	}
	cfg.Store.MinConns = minConns

	connectTimeout, err := getDuration(lookup, "BN_CONNECT_TIMEOUT")
	if err != nil {
		return Config{}, err
	}
	cfg.Store.ConnectTimeout = connectTimeout

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("config: BN_ADDR is required")
	}
	if strings.TrimSpace(c.ProjectPrefix) == "" {
		return fmt.Errorf("config: BN_PROJECT_PREFIX is required")
	}
	if strings.TrimSpace(c.Actor) == "" {
		return fmt.Errorf("config: BN_ACTOR is required")
	}
	if err := c.Store.Validate(); err != nil {
		return err
	}
	return nil
}

func getString(lookup LookupFunc, key, fallback string) string {
	if value, ok := lookup(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

// resolveDSN reads the database DSN from either BN_DSN (inline) or BN_DSN_FILE
// (path to a file holding the DSN). The file form keeps the secret out of the
// process environment, argv, and any rendered compose config — see
// .agents/plans/deploy/. Exactly one source may be set; setting both is an
// error. A read failure is reported without revealing file contents. An empty
// or whitespace-only result falls through to Store.Validate, which returns
// ErrEmptyDSN.
func resolveDSN(lookup LookupFunc) (string, error) {
	inline, inlineSet := lookupNonEmpty(lookup, "BN_DSN")
	path, pathSet := lookupNonEmpty(lookup, "BN_DSN_FILE")

	if !pathSet {
		return inline, nil
	}
	if inlineSet {
		return "", fmt.Errorf("config: set only one of BN_DSN or BN_DSN_FILE, not both")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// %w carries the path (not secret) but never the DSN contents.
		return "", fmt.Errorf("config: reading BN_DSN_FILE: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// lookupNonEmpty returns the trimmed value for key and whether it was both
// present and non-empty after trimming.
func lookupNonEmpty(lookup LookupFunc, key string) (string, bool) {
	if value, ok := lookup(key); ok {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed, true
		}
	}
	return "", false
}

func getInt32(lookup LookupFunc, key string) (int32, error) {
	raw, ok := lookup(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a 32-bit integer: %w", key, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("config: %s must be non-negative", key)
	}
	return int32(parsed), nil
}

func getDuration(lookup LookupFunc, key string) (time.Duration, error) {
	raw, ok := lookup(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a duration: %w", key, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("config: %s must be non-negative", key)
	}
	return parsed, nil
}
