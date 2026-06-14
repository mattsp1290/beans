package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	beansmodel "github.com/mattsp1290/beans/model"
	beansstore "github.com/mattsp1290/beans/store"
)

func TestSentinelReExports(t *testing.T) {
	tests := []struct {
		name string
		got  error
		want error
	}{
		{"not found", ErrNotFound, beansstore.ErrNotFound},
		{"cycle", ErrCycle, beansstore.ErrCycle},
		{"duplicate dep", ErrDuplicateDep, beansstore.ErrDuplicateDep},
		{"conflict", ErrConflict, beansstore.ErrConflict},
		{"disabled", ErrDisabled, beansstore.ErrDisabled},
		{"empty dsn", ErrEmptyDSN, beansstore.ErrEmptyDSN},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.got, tt.want) {
				t.Fatalf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestUnsupportedDriverCompatibilitySentinel(t *testing.T) {
	if ErrUnsupportedDriver == nil {
		t.Fatal("ErrUnsupportedDriver is nil")
	}
	if got := ErrUnsupportedDriver.Error(); got != "store: unsupported database driver" {
		t.Fatalf("ErrUnsupportedDriver = %q, want stable compatibility message", got)
	}
}

func TestAdapterCopiesStateSlices(t *testing.T) {
	terminal := []beansmodel.IssueState{"closed"}
	active := []beansmodel.IssueState{"open"}
	adapter := NewAdapterFromStore(nil, "bc", terminal, active)

	terminal[0] = "mutated"
	active[0] = "mutated"

	if got := adapter.TerminalStates(); len(got) != 1 || got[0] != "closed" {
		t.Fatalf("terminal states = %v, want [closed]", got)
	}
	if got := adapter.ActiveStates(); len(got) != 1 || got[0] != "open" {
		t.Fatalf("active states = %v, want [open]", got)
	}

	returned := adapter.TerminalStates()
	returned[0] = "mutated"
	if got := adapter.TerminalStates(); len(got) != 1 || got[0] != "closed" {
		t.Fatalf("terminal states after caller mutation = %v, want [closed]", got)
	}
}

func TestNewAdapterWrapsStoreErrors(t *testing.T) {
	_, err := NewAdapter(context.Background(), AdapterConfig{
		Store: Config{},
	})
	if !errors.Is(err, ErrEmptyDSN) {
		t.Fatalf("NewAdapter error = %v, want ErrEmptyDSN", err)
	}
}

func TestAdapterEnsureProjectRegistersPrefix(t *testing.T) {
	ctx := context.Background()
	adapter, err := NewAdapter(ctx, AdapterConfig{
		Store: Config{
			Driver: DriverSQLite,
			DSN:    SecretDSN(fmt.Sprintf("file:%s", filepath.Join(t.TempDir(), "adapter.sqlite"))),
		},
		ProjectPrefix: "bc",
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	defer adapter.Close()

	if err := adapter.EnsureProject(ctx); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	exists, err := adapter.Store().ProjectExists(ctx, "bc")
	if err != nil {
		t.Fatalf("ProjectExists: %v", err)
	}
	if !exists {
		t.Fatal("project bc does not exist after EnsureProject")
	}
}
