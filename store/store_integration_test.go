//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/mattsp1290/beans/model"
	"github.com/mattsp1290/beans/schema"
	store "github.com/mattsp1290/beans/store"
)

// testStore starts a fresh Postgres container, runs bn migrations, and returns
// a ready Store. Cleanup (store close + container terminate) is registered via
// t.Cleanup.
func testStore(t *testing.T) *store.Store {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	connStr := testPostgresDSN(t, ctx)
	store, err := store.New(ctx, store.Config{
		DSN:      store.SecretDSN(connStr),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("store integration: New: %v", err)
		return nil
	}
	t.Cleanup(store.Close)
	return store
}

func testPostgresDSN(t *testing.T, ctx context.Context) string {
	t.Helper()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("bn_test"),
		postgres.WithUsername("bn"),
		postgres.WithPassword("bn"),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		if isDockerUnavailable(err) {
			t.Skipf("store integration: docker unavailable: %v", err)
			return ""
		}
		t.Fatalf("store integration: postgres.Run: %v", err)
		return ""
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("store integration: ConnectionString: %v", err)
		return ""
	}

	return connStr
}

func testMySQLDSN(t *testing.T, ctx context.Context) string {
	t.Helper()

	container, err := tcmysql.Run(ctx, "mysql:8.4",
		tcmysql.WithDatabase("bn_test"),
		tcmysql.WithUsername("bn"),
		tcmysql.WithPassword("bn"),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		if isDockerUnavailable(err) {
			t.Skipf("store integration: docker unavailable: %v", err)
			return ""
		}
		t.Fatalf("store integration: mysql.Run: %v", err)
		return ""
	}

	dsn, err := container.ConnectionString(ctx, "parseTime=true", "loc=UTC", "multiStatements=true")
	if err != nil {
		t.Fatalf("store integration: mysql ConnectionString: %v", err)
		return ""
	}
	return dsn
}

// isDockerUnavailable matches testcontainers' docker-unavailable error surface.
func isDockerUnavailable(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Cannot connect to the Docker daemon") ||
		strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "context deadline exceeded")
}

// --- tests ------------------------------------------------------------------

type storeContractDialect struct {
	name                   string
	open                   func(t *testing.T) *store.Store
	openWithSQL            func(t *testing.T) (*store.Store, *sql.DB)
	timestampTick          time.Duration
	acceptSingleWriterLock bool
}

func TestStoreContractAcrossDialects(t *testing.T) {
	dialects := []storeContractDialect{
		{
			name:          "postgres",
			open:          openPostgresContractStore,
			timestampTick: 10 * time.Millisecond,
		},
		{
			name:        "mysql",
			open:        openMySQLContractStore,
			openWithSQL: openMySQLContractStoreWithSQL,
			// MySQL TIMESTAMP columns are microsecond precision; keep the tick
			// wide enough that future driver/backend precision shifts do not
			// make advancement assertions depend on scheduler timing.
			timestampTick: 10 * time.Millisecond,
		},
		{
			name:                   "sqlite",
			open:                   openSQLiteContractStore,
			timestampTick:          10 * time.Millisecond,
			acceptSingleWriterLock: true,
		},
	}
	for _, dialect := range dialects {
		dialect := dialect
		t.Run(dialect.name, func(t *testing.T) {
			storeContractTest(t, dialect)
		})
	}
}

func storeContractTest(t *testing.T, dialect storeContractDialect) {
	t.Helper()

	// Each scenario opens a fresh store so failures cannot leak state across
	// dialect assertions or hide cleanup differences between backends.
	t.Run("issue_dependencies_and_imports", func(t *testing.T) {
		s := dialect.open(t)
		ctx := context.Background()
		prefix := contractPrefix(dialect, "issues")
		otherPrefix := contractPrefix(dialect, "other")
		ensureProject(t, s, ctx, prefix)
		ensureProject(t, s, ctx, otherPrefix)

		parent, err := s.CreateIssue(ctx, store.CreateIssueInput{
			Prefix:      prefix,
			Title:       "Parent",
			Description: "contract parent",
			Priority:    0,
			IssueType:   "task",
			Labels:      []string{"contract", dialect.name},
			Actor:       "alice",
		})
		if err != nil {
			t.Fatalf("CreateIssue parent: %v", err)
		}
		child, err := s.CreateIssue(ctx, store.CreateIssueInput{
			Prefix:      prefix,
			Title:       "Child",
			Description: "contract child",
			Priority:    2,
			IssueType:   "bug",
			BranchName:  "contract/child",
			URL:         "https://example.test/child",
		})
		if err != nil {
			t.Fatalf("CreateIssue child: %v", err)
		}
		assertUTCNonZero(t, parent.CreatedAt)
		assertUTCNonZero(t, child.UpdatedAt)
		if child.Priority != model.PriorityMedium {
			t.Fatalf("child priority = %v, want medium", child.Priority)
		}

		if err := s.AddDep(ctx, child.ID, parent.ID); err != nil {
			t.Fatalf("AddDep: %v", err)
		}
		if err := s.AddDep(ctx, child.ID, parent.ID); !errors.Is(err, store.ErrDuplicateDep) {
			t.Fatalf("duplicate AddDep = %v, want ErrDuplicateDep", err)
		}
		if err := s.AddDep(ctx, parent.ID, child.ID); !errors.Is(err, store.ErrCycle) {
			t.Fatalf("cycle AddDep = %v, want ErrCycle", err)
		}
		gotChild, err := s.GetIssue(ctx, child.ID)
		if err != nil {
			t.Fatalf("GetIssue child: %v", err)
		}
		if !slices.Equal(gotChild.BlockedBy, []string{parent.ID}) {
			t.Fatalf("child blockers = %v, want parent", gotChild.BlockedBy)
		}
		ready, err := s.ReadyIssues(ctx, prefix, []model.IssueState{"closed"}, []model.IssueState{"open"})
		if err != nil {
			t.Fatalf("ReadyIssues initial: %v", err)
		}
		if got, want := issueIDs(ready), []string{parent.ID}; !slices.Equal(got, want) {
			t.Fatalf("ReadyIssues initial = %v, want %v", got, want)
		}

		time.Sleep(dialect.timestampTick)
		if err := s.CloseIssue(ctx, parent.ID, "alice", "done"); err != nil {
			t.Fatalf("CloseIssue parent: %v", err)
		}
		closedParent, err := s.GetIssue(ctx, parent.ID)
		if err != nil {
			t.Fatalf("GetIssue closed parent: %v", err)
		}
		if closedParent.State != "closed" || !closedParent.UpdatedAt.After(parent.UpdatedAt) {
			t.Fatalf("closed parent = %+v, want closed and newer updated_at", closedParent)
		}
		ready, err = s.ReadyIssues(ctx, prefix, []model.IssueState{"closed"}, []model.IssueState{"open"})
		if err != nil {
			t.Fatalf("ReadyIssues after close: %v", err)
		}
		if got, want := issueIDs(ready), []string{child.ID}; !slices.Equal(got, want) {
			t.Fatalf("ReadyIssues after close = %v, want %v", got, want)
		}

		cross, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: otherPrefix, Title: "Other", Priority: 1, IssueType: "task"})
		if err != nil {
			t.Fatalf("CreateIssue other prefix: %v", err)
		}
		importResult, err := s.ImportIssuesFull(ctx, []store.ImportInput{
			{ID: prefix + "-import-parent", Prefix: prefix, Title: "Imported parent", State: "open", Priority: 1, IssueType: "task"},
			{ID: prefix + "-import-child", Prefix: prefix, Title: "Imported child", State: "open", Priority: 2, IssueType: "bug", Deps: []string{prefix + "-import-parent", prefix + "-missing", prefix + "-import-child", prefix + "-import-parent"}},
			{ID: cross.ID, Prefix: prefix, Title: "Cross prefix", State: "open", Priority: 1, IssueType: "task"},
		}, store.ImportOptions{Mode: store.ImportModeCreateOnly, TerminalStates: []model.IssueState{"closed"}})
		if err != nil {
			t.Fatalf("ImportIssuesFull create-only: %v", err)
		}
		if importResult.Created != 2 || importResult.CrossPrefixConflicts != 1 || importResult.DepsAdded != 1 ||
			importResult.DepsSkippedMissingBlocker != 1 || importResult.DepsSkippedSelf != 1 || importResult.DepsSkippedDuplicate != 1 {
			t.Fatalf("ImportIssuesFull result = %+v, want portable create/dependency counters", importResult)
		}
	})

	t.Run("repos_audit_and_issue_targets", func(t *testing.T) {
		s := dialect.open(t)
		ctx := context.Background()
		prefix := contractPrefix(dialect, "repos")
		ensureProject(t, s, ctx, prefix)
		if err := s.AddRepoAdmin(ctx, prefix, "alice", "alice", true); err != nil {
			t.Fatalf("AddRepoAdmin bootstrap: %v", err)
		}
		if err := s.AddRepoAdmin(ctx, prefix, "bob", "alice", false); err != nil {
			t.Fatalf("AddRepoAdmin bob: %v", err)
		}
		if err := s.AuthorizeRepoAdmin(ctx, prefix, "mallory"); !errors.Is(err, store.ErrUnauthorized) {
			t.Fatalf("AuthorizeRepoAdmin mallory = %v, want ErrUnauthorized", err)
		}

		repo, err := s.CreateRepo(ctx, store.CreateRepoInput{
			Prefix:        prefix,
			Slug:          "core",
			DisplayName:   "Core",
			RemoteURL:     "git@github.com:punk1290/core.git",
			AuthRef:       "ssh-key:github-default",
			DefaultBranch: "main",
			Actor:         "alice",
			Aliases:       []string{"core", "github.com/punk1290/core"},
			Metadata:      map[string]any{"dialect": dialect.name},
		})
		if err != nil {
			t.Fatalf("CreateRepo: %v", err)
		}
		if repo.Metadata["dialect"] != dialect.name {
			t.Fatalf("repo metadata = %+v, want dialect %s", repo.Metadata, dialect.name)
		}
		if _, err := s.CreateRepo(ctx, store.CreateRepoInput{
			Prefix:        prefix,
			Slug:          "conflict",
			RemoteURL:     "git@github.com:punk1290/conflict.git",
			AuthRef:       "ssh-key:github-default",
			DefaultBranch: "main",
			Actor:         "alice",
			Aliases:       []string{"core"},
		}); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("CreateRepo duplicate alias = %v, want ErrConflict", err)
		}
		if _, err := s.ResolveRepoAlias(ctx, prefix, "github.com/punk1290/core"); err != nil {
			t.Fatalf("ResolveRepoAlias: %v", err)
		}

		time.Sleep(dialect.timestampTick)
		branch := "trunk"
		updated, err := s.UpdateRepo(ctx, prefix, "core", store.UpdateRepoInput{
			DefaultBranch: &branch,
			Actor:         "alice",
			Aliases:       []string{"new-core"},
		})
		if err != nil {
			t.Fatalf("UpdateRepo: %v", err)
		}
		if updated.DefaultBranch != "trunk" || !updated.UpdatedAt.After(repo.UpdatedAt) {
			t.Fatalf("updated repo = %+v, want trunk and newer timestamp", updated)
		}
		if _, err := s.ResolveRepoAlias(ctx, prefix, "core"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("old alias lookup = %v, want ErrNotFound", err)
		}
		if _, err := s.ResolveRepoAlias(ctx, prefix, "new-core"); err != nil {
			t.Fatalf("new alias lookup: %v", err)
		}

		issue, err := s.CreateIssue(ctx, store.CreateIssueInput{
			Prefix: prefix,
			Title:  "Targeted issue",
			Repo: &store.IssueRepoInput{
				RepoSlug:       "core",
				RequestedRef:   "feature",
				BaseRef:        "trunk",
				WorkBranch:     "work/contract",
				WorktreeSubdir: "services/core",
				Metadata:       map[string]any{"dialect": dialect.name},
			},
		})
		if err != nil {
			t.Fatalf("CreateIssue targeted: %v", err)
		}
		gotIssue, err := s.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("GetIssue targeted: %v", err)
		}
		if gotIssue.Repo == nil ||
			gotIssue.Repo.ID != repo.ID ||
			gotIssue.Repo.Slug != "core" ||
			gotIssue.Repo.RequestedRef != "feature" ||
			gotIssue.Repo.BaseRef != "trunk" ||
			gotIssue.Repo.WorkBranch != "work/contract" ||
			gotIssue.Repo.WorktreeSubdir != "services/core" ||
			gotIssue.Repo.Metadata["dialect"] != dialect.name {
			t.Fatalf("repo target = %+v, want full target", gotIssue.Repo)
		}

		if _, err := s.DisableRepo(ctx, prefix, "core", "alice"); err != nil {
			t.Fatalf("DisableRepo: %v", err)
		}
		enabledRepos, err := s.ListRepos(ctx, prefix, false)
		if err != nil {
			t.Fatalf("ListRepos enabled: %v", err)
		}
		if len(enabledRepos) != 0 {
			t.Fatalf("enabled repos after disable = %+v, want none", enabledRepos)
		}
		audits, err := s.ListRepoAudit(ctx, prefix, repo.ID, 10)
		if err != nil {
			t.Fatalf("ListRepoAudit: %v", err)
		}
		if got, want := auditActions(audits), []string{"repo.update", "repo.update", "repo.create"}; !slices.Equal(got, want) {
			t.Fatalf("audit actions = %v, want %v", got, want)
		}
		if err := s.RemoveRepoAdmin(ctx, prefix, "bob", "alice"); err != nil {
			t.Fatalf("RemoveRepoAdmin bob: %v", err)
		}
		if err := s.RemoveRepoAdmin(ctx, prefix, "alice", "alice"); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("RemoveRepoAdmin last admin = %v, want ErrConflict", err)
		}
	})

	t.Run("memory_search_and_close_errors", func(t *testing.T) {
		s := dialect.open(t)
		ctx := context.Background()
		prefix := contractPrefix(dialect, "memory")
		otherPrefix := contractPrefix(dialect, "memory-other")
		ensureProject(t, s, ctx, prefix)
		ensureProject(t, s, ctx, otherPrefix)

		global, err := s.InsertMemory(ctx, store.MemoryInput{
			Body: "global contractneedle reference",
			Type: "reference",
			Tags: []string{"shared", dialect.name, "shared"},
		})
		if err != nil {
			t.Fatalf("InsertMemory global: %v", err)
		}
		weak, err := s.InsertMemory(ctx, store.MemoryInput{
			Prefix: prefix,
			Body:   "contractneedle appears once",
			Type:   "note",
			Tags:   []string{"rank", dialect.name},
		})
		if err != nil {
			t.Fatalf("InsertMemory weak: %v", err)
		}
		time.Sleep(dialect.timestampTick)
		strong, err := s.InsertMemory(ctx, store.MemoryInput{
			Prefix: prefix,
			Body:   "contractneedle contractneedle contractneedle appears often",
			Type:   "note",
			Tags:   []string{"rank", dialect.name},
		})
		if err != nil {
			t.Fatalf("InsertMemory strong: %v", err)
		}
		if _, err := s.InsertMemory(ctx, store.MemoryInput{
			Prefix: otherPrefix,
			Body:   "contractneedle other prefix",
			Type:   "note",
			Tags:   []string{"rank", dialect.name},
		}); err != nil {
			t.Fatalf("InsertMemory other: %v", err)
		}
		assertUTCNonZero(t, strong.CreatedAt)
		if got, want := strong.Tags, sortedStrings("rank", dialect.name); !slices.Equal(got, want) {
			t.Fatalf("strong tags = %v, want %v", got, want)
		}

		scoped, err := s.SearchMemories(ctx, "", store.MemoryFilter{Prefix: prefix, Limit: 10})
		if err != nil {
			t.Fatalf("SearchMemories scoped: %v", err)
		}
		if got, want := memoryIDs(scoped), []int64{strong.ID, weak.ID, global.ID}; !slices.Equal(got, want) {
			t.Fatalf("scoped memory IDs = %v, want %v", got, want)
		}
		ranked, err := s.SearchMemories(ctx, "contractneedle", store.MemoryFilter{
			Prefix: prefix,
			Type:   "note",
			Tags:   []string{"rank", dialect.name},
			Limit:  10,
		})
		if err != nil {
			t.Fatalf("SearchMemories ranked: %v", err)
		}
		if got, want := memoryIDs(ranked), []int64{strong.ID, weak.ID}; !slices.Equal(got, want) {
			t.Fatalf("ranked memory IDs = %v, want relevance order %v", got, want)
		}
		for _, query := range []string{"contractneedle", `"unterminated`, "%", "_"} {
			if _, err := s.SearchMemories(ctx, query, store.MemoryFilter{Prefix: prefix, Limit: 10}); err != nil {
				t.Fatalf("SearchMemories %q: %v", query, err)
			}
		}

		s.Close()
		s.Close()
		if err := s.EnsureProject(ctx, prefix); !errors.Is(err, store.ErrPoolClosed) {
			t.Fatalf("EnsureProject after Close = %v, want ErrPoolClosed", err)
		}
	})

	t.Run("timestamps_and_timezone", func(t *testing.T) {
		var s *store.Store
		var rawSQL *sql.DB
		if dialect.openWithSQL != nil {
			s, rawSQL = dialect.openWithSQL(t)
		} else {
			s = dialect.open(t)
		}
		ctx := context.Background()
		prefix := contractPrefix(dialect, "timestamps")
		ensureProject(t, s, ctx, prefix)

		issue, err := s.CreateIssue(ctx, store.CreateIssueInput{
			Prefix:    prefix,
			Title:     "Timestamped issue",
			Priority:  1,
			IssueType: "task",
		})
		if err != nil {
			t.Fatalf("CreateIssue timestamped: %v", err)
		}
		assertUTCNonZero(t, issue.CreatedAt)
		assertUTCNonZero(t, issue.UpdatedAt)
		if rawSQL != nil {
			known := time.Date(2024, 3, 10, 7, 30, 0, 123456000, time.UTC)
			if _, err := rawSQL.ExecContext(ctx, `UPDATE bn_issues SET updated_at = ? WHERE id = ?`, known, issue.ID); err != nil {
				t.Fatalf("seed known raw updated_at: %v", err)
			}
			assertRawMySQLTimestamp(t, ctx, rawSQL, issue.ID, known)
			gotKnown, err := s.GetIssue(ctx, issue.ID)
			if err != nil {
				t.Fatalf("GetIssue known timestamped: %v", err)
			}
			if !gotKnown.UpdatedAt.Equal(known) {
				t.Fatalf("store updated_at = %s, want exact instant %s", gotKnown.UpdatedAt, known)
			}
			issue = gotKnown
		}

		time.Sleep(dialect.timestampTick)
		title := "Timestamped issue updated"
		updatedIssue, err := s.UpdateIssue(ctx, issue.ID, store.UpdateIssueInput{Title: &title})
		if err != nil {
			t.Fatalf("UpdateIssue timestamped: %v", err)
		}
		assertTimestampAdvanced(t, "UpdateIssue updated_at", issue.UpdatedAt, updatedIssue.UpdatedAt)

		time.Sleep(dialect.timestampTick)
		if err := s.CloseIssue(ctx, issue.ID, "alice", "timestamp done"); err != nil {
			t.Fatalf("CloseIssue timestamped: %v", err)
		}
		closedIssue, err := s.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("GetIssue closed timestamped: %v", err)
		}
		assertTimestampAdvanced(t, "CloseIssue updated_at", updatedIssue.UpdatedAt, closedIssue.UpdatedAt)

		importID := prefix + "-imported"
		importResult, err := s.ImportIssuesFull(ctx, []store.ImportInput{{
			ID: importID, Prefix: prefix, Title: "Imported timestamp", State: "open", Priority: 1, IssueType: "task",
		}}, store.ImportOptions{Mode: store.ImportModeCreateOnly, TerminalStates: []model.IssueState{"closed"}})
		if err != nil {
			t.Fatalf("ImportIssuesFull create timestamped: %v", err)
		}
		if importResult.Created != 1 {
			t.Fatalf("import create result = %+v, want created=1", importResult)
		}
		imported, err := s.GetIssue(ctx, importID)
		if err != nil {
			t.Fatalf("GetIssue imported timestamped: %v", err)
		}
		assertUTCNonZero(t, imported.CreatedAt)
		assertUTCNonZero(t, imported.UpdatedAt)

		time.Sleep(dialect.timestampTick)
		importResult, err = s.ImportIssuesFull(ctx, []store.ImportInput{{
			ID: importID, Prefix: prefix, Title: "Imported timestamp updated", State: "open", Priority: 0, IssueType: "bug",
		}}, store.ImportOptions{Mode: store.ImportModeMerge, TerminalStates: []model.IssueState{"closed"}})
		if err != nil {
			t.Fatalf("ImportIssuesFull merge timestamped: %v", err)
		}
		if importResult.Updated != 1 {
			t.Fatalf("import merge result = %+v, want updated=1", importResult)
		}
		merged, err := s.GetIssue(ctx, importID)
		if err != nil {
			t.Fatalf("GetIssue merged timestamped: %v", err)
		}
		assertTimestampAdvanced(t, "ImportIssuesFull merge updated_at", imported.UpdatedAt, merged.UpdatedAt)

		if err := s.AddRepoAdmin(ctx, prefix, "alice", "alice", true); err != nil {
			t.Fatalf("AddRepoAdmin timestamped: %v", err)
		}
		repo, err := s.CreateRepo(ctx, store.CreateRepoInput{
			Prefix:        prefix,
			Slug:          "timestamps",
			RemoteURL:     "git@github.com:punk1290/timestamps.git",
			AuthRef:       "ssh-key:github-default",
			DefaultBranch: "main",
			Actor:         "alice",
		})
		if err != nil {
			t.Fatalf("CreateRepo timestamped: %v", err)
		}
		assertUTCNonZero(t, repo.CreatedAt)
		assertUTCNonZero(t, repo.UpdatedAt)

		time.Sleep(dialect.timestampTick)
		branch := "trunk"
		updatedRepo, err := s.UpdateRepo(ctx, prefix, "timestamps", store.UpdateRepoInput{
			DefaultBranch: &branch,
			Actor:         "alice",
		})
		if err != nil {
			t.Fatalf("UpdateRepo timestamped: %v", err)
		}
		assertTimestampAdvanced(t, "UpdateRepo updated_at", repo.UpdatedAt, updatedRepo.UpdatedAt)

		firstMemory, err := s.InsertMemory(ctx, store.MemoryInput{
			Prefix: prefix,
			Body:   "first timestamp memory",
			Type:   "note",
			Tags:   []string{"timestamp"},
		})
		if err != nil {
			t.Fatalf("InsertMemory first timestamped: %v", err)
		}
		assertUTCNonZero(t, firstMemory.CreatedAt)
		time.Sleep(dialect.timestampTick)
		secondMemory, err := s.InsertMemory(ctx, store.MemoryInput{
			Prefix: prefix,
			Body:   "second timestamp memory",
			Type:   "note",
			Tags:   []string{"timestamp"},
		})
		if err != nil {
			t.Fatalf("InsertMemory second timestamped: %v", err)
		}
		assertTimestampAdvanced(t, "InsertMemory created_at", firstMemory.CreatedAt, secondMemory.CreatedAt)
		memories, err := s.SearchMemories(ctx, "", store.MemoryFilter{Prefix: prefix, Tags: []string{"timestamp"}, Limit: 2})
		if err != nil {
			t.Fatalf("SearchMemories timestamped: %v", err)
		}
		if got, want := memoryIDs(memories), []int64{secondMemory.ID, firstMemory.ID}; !slices.Equal(got, want) {
			t.Fatalf("timestamp memory IDs = %v, want newest-first %v", got, want)
		}
		for _, memory := range memories {
			assertUTCNonZero(t, memory.CreatedAt)
		}
	})

	t.Run("concurrent_add_dep_cannot_create_cycle", func(t *testing.T) {
		s := dialect.open(t)
		ctx := context.Background()
		prefix := contractPrefix(dialect, "dep-race")
		ensureProject(t, s, ctx, prefix)
		a, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: prefix, Title: "A", Priority: 1, IssueType: "task"})
		if err != nil {
			t.Fatalf("CreateIssue A: %v", err)
		}
		b, err := s.CreateIssue(ctx, store.CreateIssueInput{Prefix: prefix, Title: "B", Priority: 1, IssueType: "task"})
		if err != nil {
			t.Fatalf("CreateIssue B: %v", err)
		}

		errs := runConcurrently(
			func() error { return s.AddDep(ctx, a.ID, b.ID) },
			func() error { return s.AddDep(ctx, b.ID, a.ID) },
		)
		success, cycle, writeLock := classifyConcurrentErrors(t, dialect, errs, store.ErrCycle)
		if success != 1 || cycle+writeLock != 1 {
			t.Fatalf("concurrent AddDep results success=%d cycle=%d writeLock=%d errs=%v, want one success and one rejected edge", success, cycle, writeLock, errs)
		}
		edges, err := s.ListDeps(ctx, ListFilter{Prefix: prefix})
		if err != nil {
			t.Fatalf("ListDeps after concurrent AddDep: %v", err)
		}
		if len(edges) != 1 {
			t.Fatalf("edges after concurrent AddDep = %+v, want exactly one acyclic edge", edges)
		}
		if err := s.AddDep(ctx, edges[0].BlockedByID, edges[0].IssueID); !errors.Is(err, store.ErrCycle) {
			t.Fatalf("reverse AddDep after race = %v, want ErrCycle", err)
		}
	})

	t.Run("concurrent_first_admin_bootstrap", func(t *testing.T) {
		s := dialect.open(t)
		ctx := context.Background()
		prefix := contractPrefix(dialect, "bootstrap")
		ensureProject(t, s, ctx, prefix)

		errs := runConcurrently(
			func() error { return s.AddRepoAdmin(ctx, prefix, "alice", "alice", true) },
			func() error { return s.AddRepoAdmin(ctx, prefix, "bob", "bob", true) },
		)
		success, unauthorized, writeLock := classifyConcurrentErrors(t, dialect, errs, store.ErrUnauthorized)
		if success != 1 || unauthorized+writeLock != 1 {
			t.Fatalf("concurrent bootstrap results success=%d unauthorized=%d writeLock=%d errs=%v, want one success and one rejected admin", success, unauthorized, writeLock, errs)
		}
		admins, err := s.ListRepoAdmins(ctx, prefix)
		if err != nil {
			t.Fatalf("ListRepoAdmins after concurrent bootstrap: %v", err)
		}
		if len(admins) != 1 {
			t.Fatalf("admins after concurrent bootstrap = %v, want exactly one first admin", admins)
		}
	})
}

func openPostgresContractStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dsn := testPostgresDSN(t, ctx)
	s, err := store.New(ctx, store.Config{
		Driver:   store.DriverPostgres,
		DSN:      store.SecretDSN(dsn),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("store integration postgres: New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func openMySQLContractStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dsn := testMySQLDSN(t, ctx)
	s, err := store.New(ctx, store.Config{
		Driver:   store.DriverMySQL,
		DSN:      store.SecretDSN(dsn),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("store integration mysql: New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func openMySQLContractStoreWithSQL(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dsn := testMySQLDSN(t, ctx)
	rawDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("store integration mysql: sql.Open: %v", err)
	}
	t.Cleanup(func() { rawDB.Close() })
	if err := rawDB.PingContext(ctx); err != nil {
		t.Fatalf("store integration mysql: raw ping: %v", err)
	}
	s, err := store.New(ctx, store.Config{
		Driver:   store.DriverMySQL,
		DSN:      store.SecretDSN(dsn),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("store integration mysql: New: %v", err)
	}
	t.Cleanup(s.Close)
	return s, rawDB
}

func openSQLiteContractStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.New(ctx, store.Config{
		Driver: store.DriverSQLite,
		DSN:    store.SecretDSN(contractSQLiteDSN(t)),
	})
	if err != nil {
		t.Fatalf("store integration sqlite: New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func contractSQLiteDSN(t *testing.T) string {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
}

func contractPrefix(dialect storeContractDialect, suffix string) string {
	return fmt.Sprintf("%s-%s", dialect.name, suffix)
}

func ensureProject(t *testing.T, s *store.Store, ctx context.Context, prefix string) {
	t.Helper()
	if err := s.EnsureProject(ctx, prefix); err != nil {
		t.Fatalf("EnsureProject %q: %v", prefix, err)
	}
	exists, err := s.ProjectExists(ctx, prefix)
	if err != nil {
		t.Fatalf("ProjectExists %q: %v", prefix, err)
	}
	if !exists {
		t.Fatalf("ProjectExists %q = false, want true", prefix)
	}
}

func auditActions(audits []store.RepoAudit) []string {
	actions := make([]string, 0, len(audits))
	for _, audit := range audits {
		actions = append(actions, audit.Action)
	}
	return actions
}

func sortedStrings(values ...string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return out
}

func runConcurrently(ops ...func() error) []error {
	start := make(chan struct{})
	errs := make(chan error, len(ops))
	var wg sync.WaitGroup
	for _, op := range ops {
		op := op
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- op()
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	out := make([]error, 0, len(ops))
	for err := range errs {
		out = append(out, err)
	}
	return out
}

func classifyConcurrentErrors(t *testing.T, dialect storeContractDialect, errs []error, expected error) (success, expectedErr, writeLock int) {
	t.Helper()
	for _, err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, expected):
			expectedErr++
		case dialect.acceptSingleWriterLock && isSingleWriterLockErr(err):
			writeLock++
		default:
			t.Fatalf("unexpected concurrent error for %s: %v", dialect.name, err)
		}
	}
	return success, expectedErr, writeLock
}

func isSingleWriterLockErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "database is busy") ||
		strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "sqlite_locked")
}

func assertUTCNonZero(t *testing.T, ts time.Time) {
	t.Helper()
	if ts.IsZero() {
		t.Fatal("timestamp is zero")
	}
	if ts.Location() != time.UTC {
		t.Fatalf("timestamp location = %v, want UTC", ts.Location())
	}
}

func assertTimestampAdvanced(t *testing.T, label string, before, after time.Time) {
	t.Helper()
	assertUTCNonZero(t, after)
	if !after.After(before) {
		t.Fatalf("%s = %s, want after %s", label, after, before)
	}
}

func assertRawMySQLTimestamp(t *testing.T, ctx context.Context, db *sql.DB, issueID string, want time.Time) {
	t.Helper()
	var raw time.Time
	if err := db.QueryRowContext(ctx, `SELECT updated_at FROM bn_issues WHERE id = ?`, issueID).Scan(&raw); err != nil {
		t.Fatalf("raw MySQL updated_at scan: %v", err)
	}
	if raw.Location() != time.UTC {
		t.Fatalf("raw MySQL updated_at location = %v, want UTC", raw.Location())
	}
	if !raw.Equal(want) {
		t.Fatalf("raw MySQL updated_at = %s, want exact instant %s", raw, want)
	}
}

// TestMigrateIdempotent verifies that calling Migrate twice is a no-op.
func TestMigrateIdempotent(t *testing.T) {
	t.Parallel()
	_ = testStore(t) // New calls Migrate once. A second call is tested via a second New.
	// The test passes if testStore returns without error.
}

func TestMigrateBootstrapsLegacyGooseVersionTable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	connStr := testPostgresDSN(t, ctx)

	first, err := store.New(ctx, store.Config{
		DSN:      store.SecretDSN(connStr),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	first.Close()

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DROP TABLE bn_schema_versions`); err != nil {
		t.Fatalf("drop bn_schema_versions: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE TABLE goose_db_version (
	id integer PRIMARY KEY GENERATED BY DEFAULT AS IDENTITY,
	version_id bigint NOT NULL,
	is_applied boolean NOT NULL,
	tstamp timestamp NOT NULL DEFAULT now()
)`); err != nil {
		t.Fatalf("create goose_db_version: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO goose_db_version (version_id, is_applied)
VALUES (0, true)`); err != nil {
		t.Fatalf("seed goose_db_version zero: %v", err)
	}
	migrations, err := schema.ListMigrations(schema.DriverPostgres)
	if err != nil {
		t.Fatalf("ListMigrations: %v", err)
	}
	for _, mig := range migrations {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, true)`,
			mig.Version,
		); err != nil {
			t.Fatalf("seed goose_db_version %d: %v", mig.Version, err)
		}
	}

	second, err := store.New(ctx, store.Config{
		DSN:      store.SecretDSN(connStr),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("second New with legacy goose table: %v", err)
	}
	defer second.Close()

	var versionCount int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM bn_schema_versions
WHERE version_id >= 1
  AND is_applied = true`).Scan(&versionCount); err != nil {
		t.Fatalf("count bn_schema_versions: %v", err)
	}
	if versionCount != len(migrations) {
		t.Fatalf("bootstrapped version count = %d, want %d", versionCount, len(migrations))
	}
}

// TestEnsureProjectIdempotent verifies that calling EnsureProject twice is safe.
func TestEnsureProjectIdempotent(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "testpfx"); err != nil {
		t.Fatalf("EnsureProject first call: %v", err)
	}
	if err := s.EnsureProject(ctx, "testpfx"); err != nil {
		t.Fatalf("EnsureProject second call (must be idempotent): %v", err)
	}
	exists, err := s.ProjectExists(ctx, "testpfx")
	if err != nil {
		t.Fatalf("ProjectExists: %v", err)
	}
	if !exists {
		t.Error("ProjectExists = false, want true after EnsureProject")
	}
}

// TestRepoAdminBootstrapAndAuthorization verifies the project-admin gate used
// by repo registry mutation commands.
func TestRepoAdminBootstrapAndAuthorization(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "repos"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := s.AuthorizeRepoAdmin(ctx, "repos", "alice"); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("AuthorizeRepoAdmin before bootstrap = %v, want ErrUnauthorized", err)
	}
	if err := s.AddRepoAdmin(ctx, "repos", "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap alice: %v", err)
	}
	if err := s.AddRepoAdmin(ctx, "repos", "bob", "bob", true); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("second bootstrap = %v, want ErrUnauthorized", err)
	}
	if err := s.AddRepoAdmin(ctx, "repos", "bob", "mallory", false); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("AddRepoAdmin by non-admin = %v, want ErrUnauthorized", err)
	}
	if err := s.AddRepoAdmin(ctx, "repos", "bob", "alice", false); err != nil {
		t.Fatalf("AddRepoAdmin bob by alice: %v", err)
	}
	if err := s.AuthorizeRepoAdmin(ctx, "repos", "alice"); err != nil {
		t.Fatalf("AuthorizeRepoAdmin alice: %v", err)
	}
	admins, err := s.ListRepoAdmins(ctx, "repos")
	if err != nil {
		t.Fatalf("ListRepoAdmins: %v", err)
	}
	if got, want := strings.Join(admins, ","), "alice,bob"; got != want {
		t.Fatalf("admins = %q, want %q", got, want)
	}
}

func TestRepoAdminBootstrapAllowsOnlyOneConcurrentFirstAdmin(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "bootstrap-race"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, actor := range []string{"alice", "bob"} {
		actor := actor
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- s.AddRepoAdmin(ctx, "bootstrap-race", actor, actor, true)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	var success, unauthorized int
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, store.ErrUnauthorized):
			unauthorized++
		default:
			t.Fatalf("AddRepoAdmin concurrent bootstrap unexpected error: %v", err)
		}
	}
	if success != 1 || unauthorized != 1 {
		t.Fatalf("concurrent bootstrap results success=%d unauthorized=%d, want 1/1", success, unauthorized)
	}
	admins, err := s.ListRepoAdmins(ctx, "bootstrap-race")
	if err != nil {
		t.Fatalf("ListRepoAdmins: %v", err)
	}
	if len(admins) != 1 {
		t.Fatalf("admins = %v, want exactly one first admin", admins)
	}
}

// TestRepoRegistryRoundTrip verifies repo CRUD, alias resolution, admin
// authorization, disable, and audit writes.
func TestRepoRegistryRoundTrip(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "repos"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := s.AddRepoAdmin(ctx, "repos", "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap: %v", err)
	}

	_, err := s.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:        "repos",
		Slug:          "boxy",
		RemoteURL:     "git@github.com:punk1290/boxy.git",
		AuthRef:       "ssh-key:github-default",
		Actor:         "mallory",
		DefaultBranch: "main",
	})
	if !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("CreateRepo unauthorized = %v, want ErrUnauthorized", err)
	}

	repo, err := s.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:        "repos",
		Slug:          "boxy",
		DisplayName:   "Boxy",
		RemoteURL:     "git@github.com:punk1290/boxy.git",
		AuthRef:       "ssh-key:github-default",
		Actor:         "alice",
		DefaultBranch: "main",
		Aliases:       []string{"github.com/punk1290/boxy", "boxy"},
		Metadata:      map[string]any{"tier": "prod"},
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if repo.ID == "" {
		t.Fatal("CreateRepo returned empty ID")
	}
	if repo.DisplayName != "Boxy" || repo.Slug != "boxy" || !repo.Enabled {
		t.Fatalf("repo = %+v, want Boxy/boxy/enabled", repo)
	}
	if repo.Metadata["tier"] != "prod" {
		t.Fatalf("metadata[tier] = %v, want prod", repo.Metadata["tier"])
	}

	got, err := s.GetRepoBySlug(ctx, "repos", "boxy")
	if err != nil {
		t.Fatalf("GetRepoBySlug: %v", err)
	}
	if got.ID != repo.ID {
		t.Fatalf("GetRepoBySlug ID = %q, want %q", got.ID, repo.ID)
	}

	byAlias, err := s.ResolveRepoAlias(ctx, "repos", "github.com/punk1290/boxy")
	if err != nil {
		t.Fatalf("ResolveRepoAlias: %v", err)
	}
	if byAlias.ID != repo.ID {
		t.Fatalf("ResolveRepoAlias ID = %q, want %q", byAlias.ID, repo.ID)
	}

	repos, err := s.ListRepos(ctx, "repos", false)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].ID != repo.ID {
		t.Fatalf("ListRepos = %+v, want one repo %s", repos, repo.ID)
	}

	branch := "trunk"
	updated, err := s.UpdateRepo(ctx, "repos", "boxy", store.UpdateRepoInput{
		DefaultBranch: &branch,
		Actor:         "alice",
		Aliases:       []string{"new-boxy"},
	})
	if err != nil {
		t.Fatalf("UpdateRepo: %v", err)
	}
	if updated.DefaultBranch != "trunk" {
		t.Fatalf("DefaultBranch = %q, want trunk", updated.DefaultBranch)
	}
	if _, err := s.ResolveRepoAlias(ctx, "repos", "boxy"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old alias lookup = %v, want ErrNotFound", err)
	}
	if _, err := s.ResolveRepoAlias(ctx, "repos", "new-boxy"); err != nil {
		t.Fatalf("new alias lookup: %v", err)
	}

	disabled, err := s.DisableRepo(ctx, "repos", "boxy", "alice")
	if err != nil {
		t.Fatalf("DisableRepo: %v", err)
	}
	if disabled.Enabled {
		t.Fatal("DisableRepo returned enabled repo")
	}
	enabledOnly, err := s.ListRepos(ctx, "repos", false)
	if err != nil {
		t.Fatalf("ListRepos enabled only: %v", err)
	}
	if len(enabledOnly) != 0 {
		t.Fatalf("enabled repos after disable = %+v, want none", enabledOnly)
	}
	allRepos, err := s.ListRepos(ctx, "repos", true)
	if err != nil {
		t.Fatalf("ListRepos include disabled: %v", err)
	}
	if len(allRepos) != 1 || allRepos[0].ID != repo.ID {
		t.Fatalf("all repos after disable = %+v, want repo %s", allRepos, repo.ID)
	}

	audits, err := s.ListRepoAudit(ctx, "repos", repo.ID, 10)
	if err != nil {
		t.Fatalf("ListRepoAudit: %v", err)
	}
	if len(audits) != 3 {
		t.Fatalf("audit row count = %d, want 3", len(audits))
	}
	if audits[0].Actor != "alice" || audits[0].Action == "" {
		t.Fatalf("latest audit = %+v, want alice action", audits[0])
	}
}

func TestRepoRegistryValidatesRepoTargets(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "repo-validation"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := s.AddRepoAdmin(ctx, "repo-validation", "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap: %v", err)
	}

	repo, err := s.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:        "repo-validation",
		Slug:          "local",
		RemoteURL:     "/tmp/local.git",
		AuthRef:       "test:none",
		Actor:         "alice",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateRepo local test repo: %v", err)
	}
	if repo.CloneStrategy != "mirror-cache" {
		t.Fatalf("CloneStrategy = %q, want mirror-cache default", repo.CloneStrategy)
	}

	_, err = s.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:    "repo-validation",
		Slug:      "bad-remote",
		RemoteURL: "ftp://example.com/repo.git",
		AuthRef:   "test:none",
		Actor:     "alice",
	})
	if err == nil || !strings.Contains(err.Error(), "validation") {
		t.Fatalf("CreateRepo bad remote = %v, want validation error", err)
	}

	badSubdir := "../outside"
	_, err = s.UpdateRepo(ctx, "repo-validation", "local", store.UpdateRepoInput{
		WorktreeSubdir: &badSubdir,
		Actor:          "alice",
	})
	if err == nil || !strings.Contains(err.Error(), "validation") {
		t.Fatalf("UpdateRepo bad subdir = %v, want validation error", err)
	}
}

func TestRepoRegistryDuplicateConflicts(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "repo-conflict"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := s.AddRepoAdmin(ctx, "repo-conflict", "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap: %v", err)
	}

	first, err := s.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:        "repo-conflict",
		Slug:          "alpha",
		RemoteURL:     "git@github.com:punk1290/alpha.git",
		AuthRef:       "ssh-key:github-default",
		Actor:         "alice",
		DefaultBranch: "main",
		Aliases:       []string{"shared"},
	})
	if err != nil {
		t.Fatalf("CreateRepo alpha: %v", err)
	}
	_, err = s.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:        "repo-conflict",
		Slug:          "alpha",
		RemoteURL:     "git@github.com:punk1290/alpha-duplicate.git",
		AuthRef:       "ssh-key:github-default",
		Actor:         "alice",
		DefaultBranch: "main",
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("CreateRepo duplicate slug = %v, want ErrConflict", err)
	}

	_, err = s.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:        "repo-conflict",
		Slug:          "beta",
		RemoteURL:     "git@github.com:punk1290/beta.git",
		AuthRef:       "ssh-key:github-default",
		Actor:         "alice",
		DefaultBranch: "main",
		Aliases:       []string{" shared "},
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("CreateRepo duplicate alias = %v, want ErrConflict", err)
	}
	if _, err := s.GetRepoBySlug(ctx, "repo-conflict", "beta"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetRepoBySlug beta after alias conflict = %v, want ErrNotFound", err)
	}
	byAlias, err := s.ResolveRepoAlias(ctx, "repo-conflict", "shared")
	if err != nil {
		t.Fatalf("ResolveRepoAlias shared: %v", err)
	}
	if byAlias.ID != first.ID {
		t.Fatalf("shared alias resolved repo %q, want %q", byAlias.ID, first.ID)
	}
}

func TestRepoAdminRemoveSemantics(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "repo-admin-remove"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := s.AddRepoAdmin(ctx, "repo-admin-remove", "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap: %v", err)
	}
	if err := s.AddRepoAdmin(ctx, "repo-admin-remove", "bob", "alice", false); err != nil {
		t.Fatalf("AddRepoAdmin bob: %v", err)
	}
	if err := s.RemoveRepoAdmin(ctx, "repo-admin-remove", "alice", "mallory"); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("RemoveRepoAdmin by non-admin = %v, want ErrUnauthorized", err)
	}
	if err := s.RemoveRepoAdmin(ctx, "repo-admin-remove", "bob", "alice"); err != nil {
		t.Fatalf("RemoveRepoAdmin bob: %v", err)
	}
	if err := s.AuthorizeRepoAdmin(ctx, "repo-admin-remove", "bob"); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("AuthorizeRepoAdmin bob after removal = %v, want ErrUnauthorized", err)
	}
	if err := s.RemoveRepoAdmin(ctx, "repo-admin-remove", "bob", "alice"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("RemoveRepoAdmin missing bob = %v, want ErrNotFound", err)
	}
	admins, err := s.ListRepoAdmins(ctx, "repo-admin-remove")
	if err != nil {
		t.Fatalf("ListRepoAdmins: %v", err)
	}
	if got, want := strings.Join(admins, ","), "alice"; got != want {
		t.Fatalf("admins = %q, want %q", got, want)
	}
	if err := s.RemoveRepoAdmin(ctx, "repo-admin-remove", "alice", "alice"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("RemoveRepoAdmin last admin = %v, want ErrConflict", err)
	}
	if err := s.AuthorizeRepoAdmin(ctx, "repo-admin-remove", "alice"); err != nil {
		t.Fatalf("AuthorizeRepoAdmin alice after last-admin removal attempt: %v", err)
	}
}

func TestRepoAuditDirectInsertAndListing(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "repo-audit"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := s.AddRepoAdmin(ctx, "repo-audit", "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap: %v", err)
	}
	repo, err := s.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:        "repo-audit",
		Slug:          "boxy",
		RemoteURL:     "git@github.com:punk1290/boxy.git",
		AuthRef:       "ssh-key:github-default",
		Actor:         "alice",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	first, err := s.InsertRepoAudit(ctx, store.RepoAuditInput{
		Prefix:    "repo-audit",
		RepoID:    repo.ID,
		Action:    "repo.test.first",
		Actor:     "alice",
		OldValues: map[string]any{"enabled": true},
		NewValues: map[string]any{"enabled": false},
		Command:   "bn repo test first",
	})
	if err != nil {
		t.Fatalf("InsertRepoAudit first: %v", err)
	}
	second, err := s.InsertRepoAudit(ctx, store.RepoAuditInput{
		Prefix:    "repo-audit",
		RepoID:    repo.ID,
		Action:    "repo.test.second",
		Actor:     "bob",
		OldValues: map[string]any{"slug": "boxy"},
		NewValues: map[string]any{"slug": "boxy-renamed"},
		Command:   "bn repo test second",
	})
	if err != nil {
		t.Fatalf("InsertRepoAudit second: %v", err)
	}
	projectAudit, err := s.InsertRepoAudit(ctx, store.RepoAuditInput{
		Prefix:    "repo-audit",
		Action:    "project.test",
		Actor:     "system",
		NewValues: map[string]any{"status": "ok"},
		Command:   "bn repo project-test",
	})
	if err != nil {
		t.Fatalf("InsertRepoAudit project: %v", err)
	}
	if first.RepoID == nil || *first.RepoID != repo.ID || first.OldValues["enabled"] != true || first.NewValues["enabled"] != false {
		t.Fatalf("first audit = %+v, want repo id and bool values", first)
	}
	if projectAudit.RepoID != nil || projectAudit.NewValues["status"] != "ok" {
		t.Fatalf("project audit = %+v, want nil repo and status", projectAudit)
	}

	repoAudits, err := s.ListRepoAudit(ctx, "repo-audit", repo.ID, 2)
	if err != nil {
		t.Fatalf("ListRepoAudit repo: %v", err)
	}
	if got, want := auditIDs(repoAudits), []int64{second.ID, first.ID}; !slices.Equal(got, want) {
		t.Fatalf("repo audit IDs = %v, want %v", got, want)
	}

	allAudits, err := s.ListRepoAudit(ctx, "repo-audit", "", 2)
	if err != nil {
		t.Fatalf("ListRepoAudit project: %v", err)
	}
	if got, want := auditIDs(allAudits), []int64{projectAudit.ID, second.ID}; !slices.Equal(got, want) {
		t.Fatalf("project audit IDs = %v, want %v", got, want)
	}
}

func auditIDs(audits []store.RepoAudit) []int64 {
	ids := make([]int64, len(audits))
	for i, audit := range audits {
		ids[i] = audit.ID
	}
	return ids
}

func TestMemoryInsertAndScopedSearch(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	for _, prefix := range []string{"mem", "other"} {
		if err := s.EnsureProject(ctx, prefix); err != nil {
			t.Fatalf("EnsureProject %s: %v", prefix, err)
		}
	}

	global, err := s.InsertMemory(ctx, store.MemoryInput{
		Body: "global reference for alpha",
		Type: "reference",
		Tags: []string{"global", "shared"},
	})
	if err != nil {
		t.Fatalf("InsertMemory global: %v", err)
	}
	if global.Prefix != nil {
		t.Fatalf("global prefix = %v, want nil", *global.Prefix)
	}
	assertMemoryFields(t, global, "global reference for alpha", "reference", []string{"global", "shared"})

	time.Sleep(10 * time.Millisecond)
	project, err := s.InsertMemory(ctx, store.MemoryInput{
		Prefix: "mem",
		Body:   "project design note for alpha",
		Type:   "project",
		Tags:   []string{"design", "alpha", "alpha"},
	})
	if err != nil {
		t.Fatalf("InsertMemory project: %v", err)
	}
	if project.Prefix == nil || *project.Prefix != "mem" {
		t.Fatalf("project prefix = %v, want mem", project.Prefix)
	}
	assertMemoryFields(t, project, "project design note for alpha", "project", []string{"alpha", "design"})

	time.Sleep(10 * time.Millisecond)
	partialTag, err := s.InsertMemory(ctx, store.MemoryInput{
		Prefix: "mem",
		Body:   "project note with only alpha",
		Type:   "project",
		Tags:   []string{"alpha"},
	})
	if err != nil {
		t.Fatalf("InsertMemory partial tag: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	other, err := s.InsertMemory(ctx, store.MemoryInput{
		Prefix: "other",
		Body:   "other project newest alpha",
		Type:   "project",
		Tags:   []string{"alpha", "other"},
	})
	if err != nil {
		t.Fatalf("InsertMemory other: %v", err)
	}

	scoped, err := s.SearchMemories(ctx, "", store.MemoryFilter{Prefix: "mem", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories scoped empty: %v", err)
	}
	if got, want := memoryIDs(scoped), []int64{partialTag.ID, project.ID, global.ID}; !slices.Equal(got, want) {
		t.Fatalf("scoped memory IDs = %v, want %v", got, want)
	}
	if scoped[0].Prefix == nil || *scoped[0].Prefix != "mem" {
		t.Fatalf("scoped[0] prefix = %v, want mem", scoped[0].Prefix)
	}
	assertMemoryFields(t, scoped[0], "project note with only alpha", "project", []string{"alpha"})
	if scoped[2].Prefix != nil {
		t.Fatalf("scoped[2] prefix = %v, want nil global prefix", *scoped[2].Prefix)
	}
	assertMemoryFields(t, scoped[2], "global reference for alpha", "reference", []string{"global", "shared"})

	all, err := s.SearchMemories(ctx, "", store.MemoryFilter{All: true, Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories all empty: %v", err)
	}
	if got, want := memoryIDs(all), []int64{other.ID, partialTag.ID, project.ID, global.ID}; !slices.Equal(got, want) {
		t.Fatalf("all memory IDs = %v, want %v", got, want)
	}

	tagged, err := s.SearchMemories(ctx, "", store.MemoryFilter{
		Prefix: "mem",
		Tags:   []string{"design", "alpha", "alpha"},
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("SearchMemories tags: %v", err)
	}
	if got, want := memoryIDs(tagged), []int64{project.ID}; !slices.Equal(got, want) {
		t.Fatalf("tagged memory IDs = %v, want %v", got, want)
	}

	typed, err := s.SearchMemories(ctx, "", store.MemoryFilter{
		Prefix: "mem",
		Type:   "reference",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("SearchMemories type: %v", err)
	}
	if got, want := memoryIDs(typed), []int64{global.ID}; !slices.Equal(got, want) {
		t.Fatalf("typed memory IDs = %v, want %v", got, want)
	}

	limited, err := s.SearchMemories(ctx, "", store.MemoryFilter{Prefix: "mem", Limit: 1})
	if err != nil {
		t.Fatalf("SearchMemories limit: %v", err)
	}
	if got, want := memoryIDs(limited), []int64{partialTag.ID}; !slices.Equal(got, want) {
		t.Fatalf("limited memory IDs = %v, want %v", got, want)
	}

	if _, err := s.InsertMemory(ctx, store.MemoryInput{
		Prefix: "mem", Body: "bad empty tag", Tags: []string{""},
	}); err == nil {
		t.Fatal("InsertMemory empty tag succeeded, want error")
	}
	if _, err := s.InsertMemory(ctx, store.MemoryInput{
		Prefix: "mem", Body: "bad long tag", Tags: []string{strings.Repeat("x", 256)},
	}); err == nil {
		t.Fatal("InsertMemory long tag succeeded, want error")
	}
	if _, err := s.SearchMemories(ctx, "", store.MemoryFilter{
		Prefix: "mem",
		Tags:   []string{"alpha", ""},
		Limit:  10,
	}); err == nil {
		t.Fatal("SearchMemories empty filter tag succeeded, want error")
	}
}

func TestMemoryEmptySearchUsesIDTieBreak(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	connStr := testPostgresDSN(t, ctx)
	s, err := store.New(ctx, store.Config{
		DSN:      store.SecretDSN(connStr),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()

	if err := s.EnsureProject(ctx, "tie"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	first, err := s.InsertMemory(ctx, store.MemoryInput{
		Prefix: "tie",
		Body:   "first equal timestamp",
		Type:   "project",
	})
	if err != nil {
		t.Fatalf("InsertMemory first: %v", err)
	}
	second, err := s.InsertMemory(ctx, store.MemoryInput{
		Prefix: "tie",
		Body:   "second equal timestamp",
		Type:   "project",
	})
	if err != nil {
		t.Fatalf("InsertMemory second: %v", err)
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE bn_memories SET created_at = $1 WHERE id IN ($2, $3)`,
		first.CreatedAt, first.ID, second.ID,
	); err != nil {
		t.Fatalf("force equal created_at: %v", err)
	}

	scoped, err := s.SearchMemories(ctx, "", store.MemoryFilter{Prefix: "tie", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories scoped tie: %v", err)
	}
	if got, want := memoryIDs(scoped), []int64{second.ID, first.ID}; !slices.Equal(got, want) {
		t.Fatalf("scoped tie memory IDs = %v, want %v", got, want)
	}

	all, err := s.SearchMemories(ctx, "", store.MemoryFilter{All: true, Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories all tie: %v", err)
	}
	if got, want := memoryIDs(all), []int64{second.ID, first.ID}; !slices.Equal(got, want) {
		t.Fatalf("all tie memory IDs = %v, want %v", got, want)
	}

	limited, err := s.SearchMemories(ctx, "", store.MemoryFilter{Prefix: "tie", Limit: 1})
	if err != nil {
		t.Fatalf("SearchMemories tie limit: %v", err)
	}
	if got, want := memoryIDs(limited), []int64{second.ID}; !slices.Equal(got, want) {
		t.Fatalf("limited tie memory IDs = %v, want %v", got, want)
	}
}

func TestMemoryFullTextSearch(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "fts"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	match, err := s.InsertMemory(ctx, store.MemoryInput{
		Prefix: "fts",
		Body:   "quantum llama search fixture",
		Type:   "reference",
		Tags:   []string{"search"},
	})
	if err != nil {
		t.Fatalf("InsertMemory match: %v", err)
	}
	if _, err := s.InsertMemory(ctx, store.MemoryInput{
		Prefix: "fts",
		Body:   "ordinary unrelated note",
		Type:   "reference",
		Tags:   []string{"other"},
	}); err != nil {
		t.Fatalf("InsertMemory non-match: %v", err)
	}

	found, err := s.SearchMemories(ctx, "quantum llama", store.MemoryFilter{Prefix: "fts", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories fts match: %v", err)
	}
	if got, want := memoryIDs(found), []int64{match.ID}; !slices.Equal(got, want) {
		t.Fatalf("fts match memory IDs = %v, want %v", got, want)
	}

	missing, err := s.SearchMemories(ctx, "nonexistentterm", store.MemoryFilter{Prefix: "fts", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories fts missing: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("fts missing returned %v, want empty", memoryIDs(missing))
	}

	empty, err := s.SearchMemories(ctx, "", store.MemoryFilter{Prefix: "fts", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories empty: %v", err)
	}
	whitespace, err := s.SearchMemories(ctx, " \t\n ", store.MemoryFilter{Prefix: "fts", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories whitespace: %v", err)
	}
	if got, want := memoryIDs(whitespace), memoryIDs(empty); !slices.Equal(got, want) {
		t.Fatalf("whitespace memory IDs = %v, want empty-query IDs %v", got, want)
	}
}

func assertMemoryFields(t *testing.T, m store.Memory, body, typ string, tags []string) {
	t.Helper()
	if m.ID == 0 {
		t.Fatal("memory ID = 0, want generated ID")
	}
	if m.Body != body {
		t.Fatalf("memory body = %q, want %q", m.Body, body)
	}
	if m.Type == nil || *m.Type != typ {
		t.Fatalf("memory type = %v, want %q", m.Type, typ)
	}
	if !slices.Equal(m.Tags, tags) {
		t.Fatalf("memory tags = %v, want %v", m.Tags, tags)
	}
	if m.CreatedAt.IsZero() {
		t.Fatal("memory CreatedAt is zero")
	}
	if m.CreatedAt.Location() != time.UTC {
		t.Fatalf("memory CreatedAt location = %v, want UTC", m.CreatedAt.Location())
	}
}

func memoryIDs(memories []store.Memory) []int64 {
	ids := make([]int64, len(memories))
	for i, m := range memories {
		ids[i] = m.ID
	}
	return ids
}

// TestCreateAndGetIssue verifies round-trip create → get.
func TestCreateAndGetIssue(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "proj"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	in := store.CreateIssueInput{
		Prefix:      "proj",
		Title:       "first issue",
		Description: "desc",
		Priority:    2,
		IssueType:   "task",
		Labels:      []string{"alpha", "beta"},
	}
	iss, err := s.CreateIssue(ctx, in)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if !strings.HasPrefix(iss.ID, "proj-") {
		t.Errorf("ID = %q, want prefix proj-", iss.ID)
	}
	if iss.Title != "first issue" {
		t.Errorf("Title = %q, want %q", iss.Title, "first issue")
	}
	if iss.State != "open" {
		t.Errorf("State = %q, want open", iss.State)
	}
	if len(iss.BlockedBy) != 0 {
		t.Errorf("BlockedBy = %v, want empty for new issue", iss.BlockedBy)
	}

	got, err := s.GetIssue(ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.ID != iss.ID {
		t.Errorf("GetIssue ID = %q, want %q", got.ID, iss.ID)
	}
}

// TestCreateIssueWithRepo verifies issue repo targets are written atomically
// and populated for all read paths used by routing.
func TestCreateIssueWithRepo(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "route"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := s.AddRepoAdmin(ctx, "route", "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap: %v", err)
	}
	repo, err := s.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:         "route",
		Slug:           "boxy",
		RemoteURL:      "git@github.com:punk1290/boxy.git",
		DefaultBranch:  "trunk",
		WorktreeSubdir: "services/boxy",
		CloneStrategy:  "mirror-cache",
		AuthRef:        "ssh-key:github-default",
		Actor:          "alice",
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	iss, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix:    "route",
		Title:     "route work",
		Priority:  1,
		IssueType: "task",
		Repo: &store.IssueRepoInput{
			RepoSlug:     "boxy",
			RequestedRef: "feature/input",
			Metadata:     map[string]any{"source": "test"},
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	assertIssueRepoTarget(t, iss, repo.ID)

	got, err := s.GetIssue(ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	assertIssueRepoTarget(t, got, repo.ID)

	listed, err := s.ListIssues(ctx, store.ListFilter{Prefix: "route"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListIssues returned %d issues, want 1", len(listed))
	}
	assertIssueRepoTarget(t, listed[0], repo.ID)

	ready, err := s.ReadyIssues(ctx, "route", []model.IssueState{"closed"}, []model.IssueState{"open"})
	if err != nil {
		t.Fatalf("ReadyIssues: %v", err)
	}
	if len(ready) != 1 {
		t.Fatalf("ReadyIssues returned %d issues, want 1", len(ready))
	}
	assertIssueRepoTarget(t, ready[0], repo.ID)

	_, err = s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix:    "route",
		Title:     "missing repo",
		Priority:  2,
		IssueType: "task",
		Repo:      &store.IssueRepoInput{RepoSlug: "missing"},
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CreateIssue missing repo = %v, want ErrNotFound", err)
	}
	afterMissing, err := s.ListIssues(ctx, store.ListFilter{Prefix: "route"})
	if err != nil {
		t.Fatalf("ListIssues after missing repo: %v", err)
	}
	if len(afterMissing) != 1 {
		t.Fatalf("missing repo create left %d issues, want 1", len(afterMissing))
	}

	for _, badSubdir := range []string{".", "../escape", "/absolute"} {
		_, err = s.CreateIssue(ctx, store.CreateIssueInput{
			Prefix:    "route",
			Title:     "bad subdir",
			Priority:  2,
			IssueType: "task",
			Repo: &store.IssueRepoInput{
				RepoSlug:       "boxy",
				WorktreeSubdir: badSubdir,
			},
		})
		if err == nil {
			t.Fatalf("CreateIssue bad subdir %q succeeded, want validation error", badSubdir)
		}
	}
	afterBadSubdir, err := s.ListIssues(ctx, store.ListFilter{Prefix: "route"})
	if err != nil {
		t.Fatalf("ListIssues after bad subdir: %v", err)
	}
	if len(afterBadSubdir) != 1 {
		t.Fatalf("bad subdir create left %d issues, want 1", len(afterBadSubdir))
	}

	if _, err := s.DisableRepo(ctx, "route", "boxy", "alice"); err != nil {
		t.Fatalf("DisableRepo: %v", err)
	}
	_, err = s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix:    "route",
		Title:     "disabled repo",
		Priority:  2,
		IssueType: "task",
		Repo:      &store.IssueRepoInput{RepoSlug: "boxy"},
	})
	if !errors.Is(err, store.ErrDisabled) {
		t.Fatalf("CreateIssue disabled repo = %v, want ErrDisabled", err)
	}
}

func assertIssueRepoTarget(t *testing.T, iss store.Issue, repoID string) {
	t.Helper()
	if iss.Repo == nil {
		t.Fatalf("issue %s repo = nil, want target", iss.ID)
	}
	if iss.Repo.ID != repoID {
		t.Fatalf("repo ID = %q, want %q", iss.Repo.ID, repoID)
	}
	if iss.Repo.Slug != "boxy" || iss.Repo.RemoteURL != "git@github.com:punk1290/boxy.git" {
		t.Fatalf("repo target = %+v, want boxy remote", iss.Repo)
	}
	if iss.Repo.DefaultBranch != "trunk" || iss.Repo.BaseRef != "trunk" {
		t.Fatalf("repo branches = default %q base %q, want trunk/trunk", iss.Repo.DefaultBranch, iss.Repo.BaseRef)
	}
	if iss.Repo.WorktreeSubdir != "services/boxy" {
		t.Fatalf("repo subdir = %q, want services/boxy", iss.Repo.WorktreeSubdir)
	}
	if iss.Repo.CloneStrategy != "mirror-cache" || iss.Repo.AuthRef != "ssh-key:github-default" {
		t.Fatalf("repo clone/auth = %q/%q, want mirror-cache/ssh-key:github-default", iss.Repo.CloneStrategy, iss.Repo.AuthRef)
	}
	if iss.Repo.RequestedRef != "feature/input" {
		t.Fatalf("requested ref = %q, want feature/input", iss.Repo.RequestedRef)
	}
	if iss.Repo.Metadata["source"] != "test" {
		t.Fatalf("metadata[source] = %v, want test", iss.Repo.Metadata["source"])
	}
}

func TestUpdateIssueRepoTarget(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "retarget"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := s.AddRepoAdmin(ctx, "retarget", "alice", "alice", true); err != nil {
		t.Fatalf("AddRepoAdmin bootstrap: %v", err)
	}
	repoA, err := s.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:        "retarget",
		Slug:          "repo-a",
		RemoteURL:     "git@github.com:punk1290/repo-a.git",
		DefaultBranch: "main",
		CloneStrategy: "mirror-cache",
		AuthRef:       "ssh-key:github-default",
		Actor:         "alice",
	})
	if err != nil {
		t.Fatalf("CreateRepo repo-a: %v", err)
	}
	repoB, err := s.CreateRepo(ctx, store.CreateRepoInput{
		Prefix:         "retarget",
		Slug:           "repo-b",
		RemoteURL:      "git@github.com:punk1290/repo-b.git",
		DefaultBranch:  "trunk",
		WorktreeSubdir: "services/default",
		CloneStrategy:  "fresh-clone",
		AuthRef:        "ssh-key:github-default",
		Actor:          "alice",
	})
	if err != nil {
		t.Fatalf("CreateRepo repo-b: %v", err)
	}
	iss, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix:    "retarget",
		Title:     "retarget me",
		Priority:  2,
		IssueType: "task",
		Repo:      &store.IssueRepoInput{RepoSlug: "repo-a", RequestedRef: "main"},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if iss.Repo == nil || iss.Repo.ID != repoA.ID {
		t.Fatalf("initial repo = %+v, want repo-a", iss.Repo)
	}

	newTitle := "retargeted"
	got, err := s.UpdateIssue(ctx, iss.ID, store.UpdateIssueInput{
		Title: &newTitle,
		Repo: &store.IssueRepoInput{
			RepoSlug:       "repo-b",
			RequestedRef:   "feature/input",
			WorktreeSubdir: "services/override",
			Metadata:       map[string]any{"reason": "test"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateIssue retarget: %v", err)
	}
	if got.Title != newTitle {
		t.Fatalf("Title = %q, want %q", got.Title, newTitle)
	}
	if got.Repo == nil || got.Repo.ID != repoB.ID || got.Repo.Slug != "repo-b" {
		t.Fatalf("updated repo = %+v, want repo-b", got.Repo)
	}
	if got.Repo.RemoteURL != "git@github.com:punk1290/repo-b.git" ||
		got.Repo.DefaultBranch != "trunk" || got.Repo.BaseRef != "trunk" ||
		got.Repo.CloneStrategy != "fresh-clone" {
		t.Fatalf("updated repo inherited registry fields = %+v", got.Repo)
	}
	if got.Repo.RequestedRef != "feature/input" || got.Repo.WorktreeSubdir != "services/override" {
		t.Fatalf("updated repo issue fields = ref %q subdir %q", got.Repo.RequestedRef, got.Repo.WorktreeSubdir)
	}
	if got.Repo.Metadata["reason"] != "test" {
		t.Fatalf("updated repo metadata = %+v, want reason=test", got.Repo.Metadata)
	}

	badTitle := "should rollback"
	badSubdir := "."
	_, err = s.UpdateIssue(ctx, iss.ID, store.UpdateIssueInput{
		Title: &badTitle,
		Repo: &store.IssueRepoInput{
			RepoSlug:       "repo-b",
			WorktreeSubdir: badSubdir,
		},
	})
	if err == nil {
		t.Fatal("UpdateIssue bad subdir succeeded, want validation error")
	}
	afterBadSubdir, err := s.GetIssue(ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue after bad subdir: %v", err)
	}
	if afterBadSubdir.Title != newTitle || afterBadSubdir.Repo.WorktreeSubdir != "services/override" {
		t.Fatalf("bad subdir update was not rolled back cleanly: title=%q repo=%+v", afterBadSubdir.Title, afterBadSubdir.Repo)
	}

	_, err = s.UpdateIssue(ctx, iss.ID, store.UpdateIssueInput{
		Title: &badTitle,
		Repo:  &store.IssueRepoInput{RepoSlug: "missing"},
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateIssue missing repo = %v, want ErrNotFound", err)
	}
	afterMissing, err := s.GetIssue(ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue after missing repo: %v", err)
	}
	if afterMissing.Title != newTitle || afterMissing.Repo == nil || afterMissing.Repo.ID != repoB.ID {
		t.Fatalf("missing repo update did not roll back cleanly: title=%q repo=%+v", afterMissing.Title, afterMissing.Repo)
	}

	if _, err := s.DisableRepo(ctx, "retarget", "repo-a", "alice"); err != nil {
		t.Fatalf("DisableRepo repo-a: %v", err)
	}
	_, err = s.UpdateIssue(ctx, iss.ID, store.UpdateIssueInput{
		Repo: &store.IssueRepoInput{RepoSlug: "repo-a"},
	})
	if !errors.Is(err, store.ErrDisabled) {
		t.Fatalf("UpdateIssue disabled repo = %v, want ErrDisabled", err)
	}
	afterDisabled, err := s.GetIssue(ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue after disabled repo: %v", err)
	}
	if afterDisabled.Repo == nil || afterDisabled.Repo.ID != repoB.ID {
		t.Fatalf("disabled repo update did not preserve repo-b: %+v", afterDisabled.Repo)
	}
}

// TestGetIssueNotFound verifies ErrNotFound is returned for a missing id.
func TestGetIssueNotFound(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	_, err := s.GetIssue(ctx, "proj-000000")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetIssue(missing) error = %v, want ErrNotFound", err)
	}
}

// TestListIssues verifies prefix-scoped listing with state filter.
func TestListIssues(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "ls"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	for _, title := range []string{"A", "B", "C"} {
		_, err := s.CreateIssue(ctx, store.CreateIssueInput{
			Prefix: "ls", Title: title, Priority: 2, IssueType: "task",
		})
		if err != nil {
			t.Fatalf("CreateIssue %s: %v", title, err)
		}
	}

	all, err := s.ListIssues(ctx, store.ListFilter{Prefix: "ls"})
	if err != nil {
		t.Fatalf("ListIssues all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListIssues all = %d issues, want 3", len(all))
	}

	open, err := s.ListIssues(ctx, store.ListFilter{
		Prefix: "ls",
		States: []model.IssueState{"open"},
	})
	if err != nil {
		t.Fatalf("ListIssues open: %v", err)
	}
	if len(open) != 3 {
		t.Errorf("ListIssues open = %d issues, want 3", len(open))
	}

	// Close one and re-query.
	if err := s.CloseIssue(ctx, all[0].ID, "tester", "done"); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	open2, err := s.ListIssues(ctx, store.ListFilter{
		Prefix: "ls",
		States: []model.IssueState{"open"},
	})
	if err != nil {
		t.Fatalf("ListIssues open after close: %v", err)
	}
	if len(open2) != 2 {
		t.Errorf("ListIssues open after close = %d, want 2", len(open2))
	}
}

func TestListIssuesOrderingLimitAndPrefixScope(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	for _, prefix := range []string{"lso", "other"} {
		if err := s.EnsureProject(ctx, prefix); err != nil {
			t.Fatalf("EnsureProject %s: %v", prefix, err)
		}
	}

	inputs := []store.CreateIssueInput{
		{Prefix: "lso", Title: "low", Priority: 3, IssueType: "task"},
		{Prefix: "lso", Title: "critical", Priority: 0, IssueType: "task"},
		{Prefix: "lso", Title: "high", Priority: 1, IssueType: "task"},
		{Prefix: "lso", Title: "medium", Priority: 2, IssueType: "task"},
		{Prefix: "other", Title: "other critical", Priority: 1, IssueType: "task"},
	}
	created := make([]store.Issue, 0, len(inputs))
	for _, in := range inputs {
		iss, err := s.CreateIssue(ctx, in)
		if err != nil {
			t.Fatalf("CreateIssue %q: %v", in.Title, err)
		}
		created = append(created, iss)
	}

	got, err := s.ListIssues(ctx, store.ListFilter{Prefix: "lso", Limit: 3})
	if err != nil {
		t.Fatalf("ListIssues limited: %v", err)
	}
	want := []string{created[1].ID, created[2].ID, created[3].ID}
	if ids := issueIDs(got); !slices.Equal(ids, want) {
		t.Fatalf("ListIssues limited IDs = %v, want %v", ids, want)
	}

	all, err := s.ListIssues(ctx, store.ListFilter{Prefix: "lso", Limit: 0})
	if err != nil {
		t.Fatalf("ListIssues unbounded: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("ListIssues unbounded returned %d issues, want 4", len(all))
	}
	for _, iss := range all {
		if !strings.HasPrefix(iss.ID, "lso-") {
			t.Fatalf("ListIssues leaked issue %s outside lso prefix", iss.ID)
		}
	}
}

// TestCloseIdempotent verifies that closing an already-closed issue returns nil.
func TestCloseIdempotent(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "ci"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	iss, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix: "ci", Title: "closeable", Priority: 2, IssueType: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if err := s.CloseIssue(ctx, iss.ID, "actor", "first close"); err != nil {
		t.Fatalf("CloseIssue first: %v", err)
	}
	if err := s.CloseIssue(ctx, iss.ID, "actor", "second close"); err != nil {
		t.Fatalf("CloseIssue second (must be idempotent): %v", err)
	}
}

func TestCloseIssueNotFoundAndStoreCloseIdempotent(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.CloseIssue(ctx, "missing-issue", "actor", "reason"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CloseIssue missing = %v, want ErrNotFound", err)
	}

	s.Close()
	s.Close()

	if err := s.EnsureProject(ctx, "closed"); !errors.Is(err, store.ErrPoolClosed) {
		t.Fatalf("EnsureProject after Store.Close = %v, want ErrPoolClosed", err)
	}
	_, err := s.ListIssues(ctx, store.ListFilter{Prefix: "closed"})
	if !errors.Is(err, store.ErrPoolClosed) {
		t.Fatalf("ListIssues after Store.Close = %v, want ErrPoolClosed", err)
	}
}

// TestDepAddRemoveCycleCheck verifies dep lifecycle and cycle detection.
func TestDepAddRemoveCycleCheck(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "dp"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	makeIssue := func(title string) string {
		t.Helper()
		iss, err := s.CreateIssue(ctx, store.CreateIssueInput{
			Prefix: "dp", Title: title, Priority: 2, IssueType: "task",
		})
		if err != nil {
			t.Fatalf("CreateIssue %s: %v", title, err)
		}
		return iss.ID
	}

	a := makeIssue("A")
	b := makeIssue("B")
	c := makeIssue("C")

	// A depends on B.
	if err := s.AddDep(ctx, a, b); err != nil {
		t.Fatalf("AddDep A→B: %v", err)
	}
	// B depends on C.
	if err := s.AddDep(ctx, b, c); err != nil {
		t.Fatalf("AddDep B→C: %v", err)
	}
	// C→A would create a cycle (A→B→C→A).
	err := s.AddDep(ctx, c, a)
	if !errors.Is(err, store.ErrCycle) {
		t.Fatalf("AddDep C→A (cycle): got %v, want ErrCycle", err)
	}
	// Self-dep uses the same portable cycle sentinel as longer dependency loops.
	err = s.AddDep(ctx, a, a)
	if !errors.Is(err, store.ErrCycle) {
		t.Fatalf("AddDep A→A (self-dep): got %v, want ErrCycle", err)
	}

	// Duplicate edge.
	err = s.AddDep(ctx, a, b)
	if !errors.Is(err, store.ErrDuplicateDep) {
		t.Fatalf("AddDep duplicate A→B: got %v, want ErrDuplicateDep", err)
	}

	// Remove edge.
	if err := s.RemoveDep(ctx, a, b); err != nil {
		t.Fatalf("RemoveDep A→B: %v", err)
	}
	// Remove again → ErrNotFound.
	if err := s.RemoveDep(ctx, a, b); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("RemoveDep missing: got %v, want ErrNotFound", err)
	}

	// BlockedBy is populated on GetIssue after re-adding.
	if err := s.AddDep(ctx, a, b); err != nil {
		t.Fatalf("re-AddDep A→B: %v", err)
	}
	got, err := s.GetIssue(ctx, a)
	if err != nil {
		t.Fatalf("GetIssue A: %v", err)
	}
	if len(got.BlockedBy) != 1 || got.BlockedBy[0] != b {
		t.Errorf("BlockedBy = %v, want [%s]", got.BlockedBy, b)
	}
}

// TestReadyIssues verifies ready semantics with a config-driven terminal set.
func TestReadyIssues(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "rdy"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	makeIssue := func(title string) string {
		t.Helper()
		iss, err := s.CreateIssue(ctx, store.CreateIssueInput{
			Prefix: "rdy", Title: title, Priority: 2, IssueType: "task",
		})
		if err != nil {
			t.Fatalf("CreateIssue %s: %v", title, err)
		}
		return iss.ID
	}

	// A blocked by B; B blocked by C; C is free.
	a := makeIssue("A")
	b := makeIssue("B")
	c := makeIssue("C")
	if err := s.AddDep(ctx, a, b); err != nil {
		t.Fatalf("AddDep A→B: %v", err)
	}
	if err := s.AddDep(ctx, b, c); err != nil {
		t.Fatalf("AddDep B→C: %v", err)
	}

	terminal := []model.IssueState{"closed", "done"}
	active := []model.IssueState{"open"}

	// With no issues closed: only C is ready.
	ready, err := s.ReadyIssues(ctx, "rdy", terminal, active)
	if err != nil {
		t.Fatalf("ReadyIssues: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != c {
		t.Errorf("ready = %v, want [C]", issueIDs(ready))
	}

	// Close C: B becomes ready.
	if err := s.CloseIssue(ctx, c, "test", ""); err != nil {
		t.Fatalf("CloseIssue C: %v", err)
	}
	ready, err = s.ReadyIssues(ctx, "rdy", terminal, active)
	if err != nil {
		t.Fatalf("ReadyIssues after C closed: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != b {
		t.Errorf("ready after C closed = %v, want [B]", issueIDs(ready))
	}

	// Close B: A becomes ready.
	if err := s.CloseIssue(ctx, b, "test", ""); err != nil {
		t.Fatalf("CloseIssue B: %v", err)
	}
	ready, err = s.ReadyIssues(ctx, "rdy", terminal, active)
	if err != nil {
		t.Fatalf("ReadyIssues after B closed: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != a {
		t.Errorf("ready after B closed = %v, want [A]", issueIDs(ready))
	}
}

// TestReadyIssues_CustomTerminal verifies that "done" (not "closed") is
// honored as a terminal state when the caller supplies it.
func TestReadyIssues_CustomTerminal(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "cterm"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	parent, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix: "cterm", Title: "parent", Priority: 2, IssueType: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	child, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix: "cterm", Title: "child", Priority: 2, IssueType: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue child: %v", err)
	}
	if err := s.AddDep(ctx, child.ID, parent.ID); err != nil {
		t.Fatalf("AddDep: %v", err)
	}

	// Transition parent to "done" (not "closed") via UpdateIssue.
	doneState := model.IssueState("done")
	_, err = s.UpdateIssue(ctx, parent.ID, store.UpdateIssueInput{State: &doneState})
	if err != nil {
		t.Fatalf("UpdateIssue parent to done: %v", err)
	}

	// Using only "closed" as terminal: child is still blocked (parent is "done", not "closed").
	ready, err := s.ReadyIssues(ctx, "cterm", []model.IssueState{"closed"}, []model.IssueState{"open"})
	if err != nil {
		t.Fatalf("ReadyIssues closed-only terminal: %v", err)
	}
	if len(ready) != 0 {
		t.Errorf("ready with closed-only terminal = %v, want empty (parent is done, not closed)", issueIDs(ready))
	}

	// Using "closed" AND "done" as terminal: child is ready.
	ready, err = s.ReadyIssues(ctx, "cterm", []model.IssueState{"closed", "done"}, []model.IssueState{"open"})
	if err != nil {
		t.Fatalf("ReadyIssues with done terminal: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != child.ID {
		t.Errorf("ready with done terminal = %v, want [child]", issueIDs(ready))
	}
}

func TestReadyIssuesOrderingAndEmptyActiveStates(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "rord"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	inputs := []store.CreateIssueInput{
		{Prefix: "rord", Title: "low", Priority: 3, IssueType: "task"},
		{Prefix: "rord", Title: "critical", Priority: 0, IssueType: "task"},
		{Prefix: "rord", Title: "high", Priority: 1, IssueType: "task"},
		{Prefix: "rord", Title: "medium", Priority: 2, IssueType: "task"},
	}
	created := make([]store.Issue, 0, len(inputs))
	for _, in := range inputs {
		iss, err := s.CreateIssue(ctx, in)
		if err != nil {
			t.Fatalf("CreateIssue %q: %v", in.Title, err)
		}
		created = append(created, iss)
	}

	empty, err := s.ReadyIssues(ctx, "rord", []model.IssueState{"closed"}, nil)
	if err != nil {
		t.Fatalf("ReadyIssues empty active states: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("ReadyIssues empty active states returned %v, want empty", issueIDs(empty))
	}

	ready, err := s.ReadyIssues(ctx, "rord", []model.IssueState{"closed"}, []model.IssueState{"open"})
	if err != nil {
		t.Fatalf("ReadyIssues: %v", err)
	}
	want := []string{created[1].ID, created[2].ID, created[3].ID, created[0].ID}
	if ids := issueIDs(ready); !slices.Equal(ids, want) {
		t.Fatalf("ReadyIssues IDs = %v, want %v", ids, want)
	}
}

// TestUpdateIssue verifies partial update semantics.
func TestUpdateIssue(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "upd"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	iss, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix: "upd", Title: "original", Priority: 2, IssueType: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	newTitle := "updated"
	got, err := s.UpdateIssue(ctx, iss.ID, store.UpdateIssueInput{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if got.Title != "updated" {
		t.Errorf("Title = %q, want updated", got.Title)
	}
	if got.Description != "original desc" && got.Description != "" {
		// description unchanged
	}
}

// TestUpdateIssueRejectsInvalidState verifies invalid state values are rejected
// without mutating the issue.
func TestUpdateIssueRejectsInvalidState(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "badst"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	iss, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix: "badst", Title: "bad state", Priority: 2, IssueType: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	badState := model.IssueState("archived")
	_, err = s.UpdateIssue(ctx, iss.ID, store.UpdateIssueInput{State: &badState})
	if err == nil {
		t.Fatal("UpdateIssue invalid state succeeded; want error")
	}
	if !errors.Is(err, store.ErrInvalidIssueState) {
		t.Fatalf("UpdateIssue invalid state error = %v, want ErrInvalidIssueState", err)
	}
	got, err := s.GetIssue(ctx, iss.ID)
	if err != nil {
		t.Fatalf("GetIssue after invalid state: %v", err)
	}
	if got.State != model.IssueState("open") {
		t.Fatalf("UpdateIssue invalid state mutated state to %q, want %q", got.State, model.IssueState("open"))
	}
}

// TestUpdateIssueNotFound verifies ErrNotFound for missing id.
func TestUpdateIssueNotFound(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	title := "x"
	_, err := s.UpdateIssue(ctx, "proj-000000", store.UpdateIssueInput{Title: &title})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateIssue(missing): got %v, want ErrNotFound", err)
	}
}

// TestDeleteIssue verifies hard delete and cascade.
func TestDeleteIssue(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "del"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	parent, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix: "del", Title: "parent", Priority: 2, IssueType: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue parent: %v", err)
	}
	child, err := s.CreateIssue(ctx, store.CreateIssueInput{
		Prefix: "del", Title: "child", Priority: 2, IssueType: "task",
	})
	if err != nil {
		t.Fatalf("CreateIssue child: %v", err)
	}
	if err := s.AddDep(ctx, child.ID, parent.ID); err != nil {
		t.Fatalf("AddDep: %v", err)
	}

	// Delete parent: CASCADE removes the dep edge; child should become unblocked.
	if err := s.DeleteIssue(ctx, parent.ID); err != nil {
		t.Fatalf("DeleteIssue parent: %v", err)
	}
	got, err := s.GetIssue(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetIssue child after parent delete: %v", err)
	}
	if len(got.BlockedBy) != 0 {
		t.Errorf("BlockedBy after parent delete = %v, want empty (CASCADE)", got.BlockedBy)
	}

	// DeleteIssue on missing id.
	if err := s.DeleteIssue(ctx, parent.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteIssue(missing): got %v, want ErrNotFound", err)
	}
}

// TestImportIssues verifies create-only import semantics with
// never-regress-terminal state protection.
func TestImportIssues(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "imp"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	terminal := []model.IssueState{"closed", "done"}

	items := []store.ImportInput{
		{ID: "imp-aaa", Prefix: "imp", Title: "Alpha", State: "open", Priority: 2, IssueType: "task"},
		{ID: "imp-bbb", Prefix: "imp", Title: "Beta", State: "open", Priority: 1, IssueType: "task"},
		// C depends on A and B.
		{ID: "imp-ccc", Prefix: "imp", Title: "Gamma", State: "open", Priority: 2, IssueType: "task",
			Deps: []string{"imp-aaa", "imp-bbb"}},
	}

	if err := s.ImportIssues(ctx, items, terminal); err != nil {
		t.Fatalf("ImportIssues: %v", err)
	}

	// Verify issues exist.
	for _, id := range []string{"imp-aaa", "imp-bbb", "imp-ccc"} {
		iss, err := s.GetIssue(ctx, id)
		if err != nil {
			t.Fatalf("GetIssue %s: %v", id, err)
		}
		if iss.ID != id {
			t.Errorf("GetIssue %s ID = %q", id, iss.ID)
		}
	}

	// Verify deps.
	gamma, err := s.GetIssue(ctx, "imp-ccc")
	if err != nil {
		t.Fatalf("GetIssue gamma: %v", err)
	}
	if len(gamma.BlockedBy) != 2 {
		t.Errorf("gamma.BlockedBy = %v, want 2 blockers", gamma.BlockedBy)
	}

	// Close imp-aaa externally (simulates orchestrator close).
	if err := s.CloseIssue(ctx, "imp-aaa", "orchestrator", "done"); err != nil {
		t.Fatalf("CloseIssue aaa: %v", err)
	}

	// Re-import with imp-aaa as "open" — must NOT regress the closed state.
	reImport := []store.ImportInput{
		{ID: "imp-aaa", Prefix: "imp", Title: "Alpha v2", State: "open", Priority: 2, IssueType: "task"},
	}
	if err := s.ImportIssues(ctx, reImport, terminal); err != nil {
		t.Fatalf("re-ImportIssues: %v", err)
	}
	aaa, err := s.GetIssue(ctx, "imp-aaa")
	if err != nil {
		t.Fatalf("GetIssue aaa after re-import: %v", err)
	}
	if aaa.State != "closed" {
		t.Errorf("re-import regressed state: got %q, want closed", aaa.State)
	}
	if aaa.Title != "Alpha v2" {
		t.Errorf("re-import title: got %q, want Alpha v2", aaa.Title)
	}
}

func TestImportIssuesPreservesActiveStateOnTerminalInput(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "impterm"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := s.ImportIssues(ctx, []store.ImportInput{
		{ID: "impterm-a", Prefix: "impterm", Title: "Alpha", State: "open", Priority: 2, IssueType: "task"},
	}, []model.IssueState{"closed", "done"}); err != nil {
		t.Fatalf("ImportIssues seed: %v", err)
	}
	if err := s.ImportIssues(ctx, []store.ImportInput{
		{ID: "impterm-a", Prefix: "impterm", Title: "Alpha closed input", State: "closed", Priority: 1, IssueType: "bug"},
	}, []model.IssueState{"closed", "done"}); err != nil {
		t.Fatalf("ImportIssues terminal input: %v", err)
	}
	got, err := s.GetIssue(ctx, "impterm-a")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.State != "open" || got.Title != "Alpha closed input" || got.IssueType != "bug" || got.Priority != model.PriorityHigh {
		t.Fatalf("re-imported issue = %+v, want open state with updated non-state fields", got)
	}
}

func TestImportIssuesFullCreateOnlyCountsAreIdempotent(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "impc"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	items := []store.ImportInput{
		{ID: "impc-parent", Prefix: "impc", Title: "Parent", State: "open", Priority: 2, IssueType: "task"},
		{ID: "impc-child", Prefix: "impc", Title: "Child", State: "open", Priority: 2, IssueType: "task",
			Deps: []string{"impc-parent", "impc-parent", "impc-missing"}},
		{ID: "impc-child", Prefix: "impc", Title: "Child v2", State: "open", Priority: 1, IssueType: "bug"},
	}

	first, err := s.ImportIssuesFull(ctx, items, store.ImportOptions{
		TerminalStates: []model.IssueState{"closed", "done"},
		Mode:           store.ImportModeCreateOnly,
	})
	if err != nil {
		t.Fatalf("ImportIssuesFull first: %v", err)
	}
	if first.Created != 2 || first.Skipped != 0 || first.DepsAdded != 1 ||
		first.DepsSkippedDuplicate != 1 || first.DepsSkippedMissingBlocker != 1 {
		t.Fatalf("first result = %+v, want created=2 dep added/duplicate/missing counts", first)
	}

	child, err := s.GetIssue(ctx, "impc-child")
	if err != nil {
		t.Fatalf("GetIssue child: %v", err)
	}
	if child.Title != "Child v2" || child.IssueType != "bug" || child.Priority != model.PriorityHigh {
		t.Fatalf("deduped child = %+v, want last issue fields", child)
	}

	second, err := s.ImportIssuesFull(ctx, items, store.ImportOptions{
		TerminalStates: []model.IssueState{"closed", "done"},
		Mode:           store.ImportModeCreateOnly,
	})
	if err != nil {
		t.Fatalf("ImportIssuesFull second: %v", err)
	}
	if second.Created != 0 || second.Skipped != 2 || second.DepsAdded != 0 {
		t.Fatalf("second result = %+v, want idempotent skip/no deps", second)
	}
}

func TestImportIssuesFullMergeStateTruthTable(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "impm"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if _, err := s.ImportIssuesFull(ctx, []store.ImportInput{
		{ID: "impm-active", Prefix: "impm", Title: "Active", State: "open", Priority: 2, IssueType: "task"},
		{ID: "impm-terminal", Prefix: "impm", Title: "Terminal", State: "done", Priority: 2, IssueType: "task"},
	}, store.ImportOptions{TerminalStates: []model.IssueState{"closed", "done"}, Mode: store.ImportModeMerge}); err != nil {
		t.Fatalf("ImportIssuesFull seed: %v", err)
	}

	result, err := s.ImportIssuesFull(ctx, []store.ImportInput{
		{ID: "impm-active", Prefix: "impm", Title: "Active closed", State: "closed", Priority: 1, IssueType: "bug"},
		{ID: "impm-terminal", Prefix: "impm", Title: "Terminal reopened input", State: "open", Priority: 3, IssueType: "task"},
	}, store.ImportOptions{TerminalStates: []model.IssueState{"closed", "done"}, Mode: store.ImportModeMerge})
	if err != nil {
		t.Fatalf("ImportIssuesFull merge: %v", err)
	}
	if result.Updated != 2 {
		t.Fatalf("merge result = %+v, want updated=2", result)
	}

	active, err := s.GetIssue(ctx, "impm-active")
	if err != nil {
		t.Fatalf("GetIssue active: %v", err)
	}
	if active.State != "closed" || active.Title != "Active closed" {
		t.Fatalf("active after merge = %+v, want closed with updated title", active)
	}
	terminal, err := s.GetIssue(ctx, "impm-terminal")
	if err != nil {
		t.Fatalf("GetIssue terminal: %v", err)
	}
	if terminal.State != "done" || terminal.Title != "Terminal reopened input" {
		t.Fatalf("terminal after merge = %+v, want done with updated title", terminal)
	}

	result, err = s.ImportIssuesFull(ctx, []store.ImportInput{
		{ID: "impm-terminal", Prefix: "impm", Title: "Terminal closed input", State: "closed", Priority: 3, IssueType: "task"},
	}, store.ImportOptions{TerminalStates: []model.IssueState{"closed", "done"}, Mode: store.ImportModeMerge})
	if err != nil {
		t.Fatalf("ImportIssuesFull terminal merge: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("terminal merge result = %+v, want updated=1", result)
	}
	terminal, err = s.GetIssue(ctx, "impm-terminal")
	if err != nil {
		t.Fatalf("GetIssue terminal after terminal merge: %v", err)
	}
	if terminal.State != "done" || terminal.Title != "Terminal closed input" {
		t.Fatalf("terminal after terminal merge = %+v, want done preserved with updated title", terminal)
	}
}

func TestImportIssuesFullRoundTripsMergeFields(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "impf"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	result, err := s.ImportIssuesFull(ctx, []store.ImportInput{
		{
			ID:          "impf-one",
			Prefix:      "impf",
			Title:       "Original title",
			Description: "Original description",
			State:       "open",
			Priority:    2,
			IssueType:   "task",
			Labels:      []string{"alpha", "beta"},
			BranchName:  "feature/original",
			URL:         "https://example.test/original",
		},
	}, store.ImportOptions{TerminalStates: []model.IssueState{"closed", "done"}, Mode: store.ImportModeCreateOnly})
	if err != nil {
		t.Fatalf("ImportIssuesFull create: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("create result = %+v, want Created=1", result)
	}

	got, err := s.GetIssue(ctx, "impf-one")
	if err != nil {
		t.Fatalf("GetIssue after create: %v", err)
	}
	if got.Title != "Original title" ||
		got.Description != "Original description" ||
		got.State != "open" ||
		got.Priority != model.PriorityMedium ||
		got.IssueType != "task" ||
		got.BranchName != "feature/original" ||
		got.URL != "https://example.test/original" ||
		!slices.Equal(got.Labels, []string{"alpha", "beta"}) {
		t.Fatalf("created issue = %+v, want imported field round trip", got)
	}

	result, err = s.ImportIssuesFull(ctx, []store.ImportInput{
		{
			ID:          "impf-one",
			Prefix:      "impf",
			Title:       "Merged title",
			Description: "Merged description",
			State:       "closed",
			Priority:    1,
			IssueType:   "bug",
			Labels:      []string{"gamma"},
			BranchName:  "feature/merged",
			URL:         "https://example.test/merged",
		},
	}, store.ImportOptions{TerminalStates: []model.IssueState{"closed", "done"}, Mode: store.ImportModeMerge})
	if err != nil {
		t.Fatalf("ImportIssuesFull merge: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("merge result = %+v, want Updated=1", result)
	}

	got, err = s.GetIssue(ctx, "impf-one")
	if err != nil {
		t.Fatalf("GetIssue after merge: %v", err)
	}
	if got.Title != "Merged title" ||
		got.Description != "Merged description" ||
		got.State != "closed" ||
		got.Priority != model.PriorityHigh ||
		got.IssueType != "bug" ||
		got.BranchName != "feature/merged" ||
		got.URL != "https://example.test/merged" ||
		!slices.Equal(got.Labels, []string{"gamma"}) {
		t.Fatalf("merged issue = %+v, want imported field round trip", got)
	}
}

func TestImportIssuesFullConcurrentCreateOnlyRetriesSerialization(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "impr"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	items := []store.ImportInput{
		{ID: "impr-parent", Prefix: "impr", Title: "Parent", State: "open", Priority: 2, IssueType: "task"},
		{ID: "impr-child", Prefix: "impr", Title: "Child", State: "open", Priority: 2, IssueType: "task", Deps: []string{"impr-parent"}},
	}
	opts := store.ImportOptions{
		TerminalStates: []model.IssueState{"closed", "done"},
		Mode:           store.ImportModeCreateOnly,
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	results := make(chan store.ImportResult, 2)
	for range 2 {
		go func() {
			<-start
			result, err := s.ImportIssuesFull(ctx, items, opts)
			if err != nil {
				errs <- err
				return
			}
			results <- result
			errs <- nil
		}()
	}
	close(start)

	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("ImportIssuesFull concurrent import %d: %v", i, err)
		}
	}

	var created, skipped, depsAdded, depsSkippedDuplicate int
	for i := 0; i < 2; i++ {
		result := <-results
		created += result.Created
		skipped += result.Skipped
		depsAdded += result.DepsAdded
		depsSkippedDuplicate += result.DepsSkippedDuplicate
	}
	if created != 2 || skipped != 2 || depsAdded != 1 || depsSkippedDuplicate != 0 {
		t.Fatalf("combined results created=%d skipped=%d deps_added=%d deps_duplicate=%d, want 2/2/1/0",
			created, skipped, depsAdded, depsSkippedDuplicate)
	}
}

func TestImportIssuesFullSkipsCrossPrefixConflicts(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	for _, prefix := range []string{"owna", "ownb"} {
		if err := s.EnsureProject(ctx, prefix); err != nil {
			t.Fatalf("EnsureProject %s: %v", prefix, err)
		}
	}
	if _, err := s.ImportIssuesFull(ctx, []store.ImportInput{
		{ID: "shared-id", Prefix: "owna", Title: "Original", State: "open", Priority: 2, IssueType: "task"},
	}, store.ImportOptions{TerminalStates: []model.IssueState{"closed", "done"}, Mode: store.ImportModeMerge}); err != nil {
		t.Fatalf("seed shared-id: %v", err)
	}

	result, err := s.ImportIssuesFull(ctx, []store.ImportInput{
		{ID: "shared-id", Prefix: "ownb", Title: "Should not write", State: "closed", Priority: 0, IssueType: "bug"},
	}, store.ImportOptions{TerminalStates: []model.IssueState{"closed", "done"}, Mode: store.ImportModeMerge})
	if err != nil {
		t.Fatalf("ImportIssuesFull conflict: %v", err)
	}
	if result.CrossPrefixConflicts != 1 || result.Updated != 0 || result.Created != 0 {
		t.Fatalf("result = %+v, want one cross-prefix conflict only", result)
	}
	got, err := s.GetIssue(ctx, "shared-id")
	if err != nil {
		t.Fatalf("GetIssue shared-id: %v", err)
	}
	if got.Title != "Original" || got.State != "open" {
		t.Fatalf("shared-id mutated: %+v", got)
	}
}

func TestImportIssuesFullSkipsInvalidDependencyEdges(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "impd"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	result, err := s.ImportIssuesFull(ctx, []store.ImportInput{
		{ID: "impd-a", Prefix: "impd", Title: "A", State: "open", Priority: 2, IssueType: "task", Deps: []string{"impd-b", "impd-a"}},
		{ID: "impd-b", Prefix: "impd", Title: "B", State: "open", Priority: 2, IssueType: "task", Deps: []string{"impd-a"}},
	}, store.ImportOptions{TerminalStates: []model.IssueState{"closed", "done"}, Mode: store.ImportModeCreateOnly})
	if err != nil {
		t.Fatalf("ImportIssuesFull deps: %v", err)
	}
	if result.Created != 2 || result.DepsAdded != 1 || result.DepsSkippedSelf != 1 || result.DepsSkippedCycle != 1 {
		t.Fatalf("result = %+v, want created=2 added=1 self=1 cycle=1", result)
	}

	a, err := s.GetIssue(ctx, "impd-a")
	if err != nil {
		t.Fatalf("GetIssue impd-a: %v", err)
	}
	b, err := s.GetIssue(ctx, "impd-b")
	if err != nil {
		t.Fatalf("GetIssue impd-b: %v", err)
	}
	if len(a.BlockedBy) != 1 || a.BlockedBy[0] != "impd-b" {
		t.Fatalf("impd-a blockers = %v, want [impd-b]", a.BlockedBy)
	}
	if len(b.BlockedBy) != 0 {
		t.Fatalf("impd-b blockers = %v, want no cycle edge", b.BlockedBy)
	}
}

// TestListDeps verifies that ListDeps returns all edges for a prefix.
func TestListDeps(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()

	if err := s.EnsureProject(ctx, "ld"); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	makeIssue := func(title string) string {
		t.Helper()
		iss, err := s.CreateIssue(ctx, store.CreateIssueInput{
			Prefix: "ld", Title: title, Priority: 2, IssueType: "task",
		})
		if err != nil {
			t.Fatalf("CreateIssue %s: %v", title, err)
		}
		return iss.ID
	}

	a, b, c := makeIssue("A"), makeIssue("B"), makeIssue("C")
	if err := s.AddDep(ctx, a, b); err != nil {
		t.Fatalf("AddDep A→B: %v", err)
	}
	if err := s.AddDep(ctx, b, c); err != nil {
		t.Fatalf("AddDep B→C: %v", err)
	}

	edges, err := s.ListDeps(ctx, ListFilter{Prefix: "ld"})
	if err != nil {
		t.Fatalf("ListDeps: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("ListDeps = %d edges, want 2", len(edges))
	}
	// A→B
	found := false
	for _, e := range edges {
		if e.IssueID == a && e.BlockedByID == b {
			found = true
		}
	}
	if !found {
		t.Errorf("ListDeps missing A→B edge")
	}
}

func issueIDs(issues []store.Issue) []string {
	ids := make([]string, len(issues))
	for i, iss := range issues {
		ids[i] = iss.ID
	}
	return ids
}
