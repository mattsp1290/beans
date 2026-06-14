package store

import (
	"context"
	"errors"
	"fmt"

	beansmodel "github.com/mattsp1290/beans/model"
	beansstore "github.com/mattsp1290/beans/store"
)

type Store = beansstore.Store
type Config = beansstore.Config
type SecretDSN = beansstore.SecretDSN
type Issue = beansstore.Issue
type ListFilter = beansstore.ListFilter
type CreateIssueInput = beansstore.CreateIssueInput
type UpdateIssueInput = beansstore.UpdateIssueInput
type DepEdge = beansstore.DepEdge

var (
	ErrNotFound          = beansstore.ErrNotFound
	ErrCycle             = beansstore.ErrCycle
	ErrDuplicateDep      = beansstore.ErrDuplicateDep
	ErrConflict          = beansstore.ErrConflict
	ErrEmptyDSN          = beansstore.ErrEmptyDSN
	ErrUnsupportedDriver = errors.New("store: unsupported database driver")
)

// NewStore constructs the underlying beans store. beans/store.New owns schema
// migrations, so bean-counter must not add its own beans-table migrations.
func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	return beansstore.New(ctx, cfg)
}

// AdapterConfig configures the application wrapper around the beans store.
type AdapterConfig struct {
	Store          Config
	ProjectPrefix  string
	TerminalStates []beansmodel.IssueState
	ActiveStates   []beansmodel.IssueState
}

// Adapter scopes store operations to one project prefix and carries the ready
// queue state sets used by ReadyIssues.
type Adapter struct {
	store          *Store
	projectPrefix  string
	terminalStates []beansmodel.IssueState
	activeStates   []beansmodel.IssueState
}

// NewAdapter dials the beans store and returns the application adapter.
func NewAdapter(ctx context.Context, cfg AdapterConfig) (*Adapter, error) {
	s, err := NewStore(ctx, cfg.Store)
	if err != nil {
		return nil, fmt.Errorf("store adapter: %w", err)
	}
	return NewAdapterFromStore(s, cfg.ProjectPrefix, cfg.TerminalStates, cfg.ActiveStates), nil
}

// NewAdapterFromStore wraps an existing Store, primarily for tests that manage
// store lifecycle externally.
func NewAdapterFromStore(
	s *Store,
	projectPrefix string,
	terminalStates []beansmodel.IssueState,
	activeStates []beansmodel.IssueState,
) *Adapter {
	return &Adapter{
		store:          s,
		projectPrefix:  projectPrefix,
		terminalStates: append([]beansmodel.IssueState(nil), terminalStates...),
		activeStates:   append([]beansmodel.IssueState(nil), activeStates...),
	}
}

// Store returns the underlying beans store for handlers that need raw CRUD
// operations.
func (a *Adapter) Store() *Store {
	if a == nil {
		return nil
	}
	return a.store
}

func (a *Adapter) ProjectPrefix() string {
	if a == nil {
		return ""
	}
	return a.projectPrefix
}

func (a *Adapter) TerminalStates() []beansmodel.IssueState {
	if a == nil {
		return nil
	}
	return append([]beansmodel.IssueState(nil), a.terminalStates...)
}

func (a *Adapter) ActiveStates() []beansmodel.IssueState {
	if a == nil {
		return nil
	}
	return append([]beansmodel.IssueState(nil), a.activeStates...)
}

// ReadyIssues returns unblocked issues for the configured project prefix.
func (a *Adapter) ReadyIssues(ctx context.Context) ([]Issue, error) {
	return a.store.ReadyIssues(ctx, a.projectPrefix, a.terminalStates, a.activeStates)
}

// ListDeps returns all dependency edges for the configured project prefix.
func (a *Adapter) ListDeps(ctx context.Context) ([]DepEdge, error) {
	return a.store.ListDeps(ctx, a.projectPrefix)
}

// Close releases database resources owned by the underlying store.
func (a *Adapter) Close() {
	if a == nil || a.store == nil {
		return
	}
	a.store.Close()
}
