package store

import "errors"

// Sentinel errors returned by Store methods. Callers branch on these with
// errors.Is rather than inspecting message text.
var (
	// ErrNotFound is returned when an operation targets an issue or project
	// that does not exist. Maps to tracker.CategoryNotFound.
	ErrNotFound = errors.New("store: not found")

	// ErrDuplicateDep is returned by AddDep when the edge already exists.
	ErrDuplicateDep = errors.New("store: dependency edge already exists")

	// ErrCycle is returned by AddDep when the proposed edge would create a
	// dependency cycle.
	ErrCycle = errors.New("store: dependency would create a cycle")

	// ErrConflict is returned when a unique constraint would be violated.
	ErrConflict = errors.New("store: conflict")

	// ErrUnauthorized is returned when an actor is not authorized to mutate
	// administrative project state such as repo registry entries.
	ErrUnauthorized = errors.New("store: unauthorized")

	// ErrDisabled is returned when an operation targets a disabled repo.
	ErrDisabled = errors.New("store: disabled")
)
