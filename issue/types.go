package issue

import "slices"

// TypesConfig is the [types] table of the hub beans.toml.
type TypesConfig struct {
	Names []string `toml:"names"`
}

// DefaultTypes is the built-in issue type vocabulary.
var DefaultTypes = []string{"task", "bug", "feature", "epic", "chore"}

// DefaultTypesConfig returns the built-in vocabulary.
func DefaultTypesConfig() TypesConfig {
	return TypesConfig{Names: slices.Clone(DefaultTypes)}
}

// ValidType reports whether typ is in the configured vocabulary.
func (c TypesConfig) ValidType(typ string) bool {
	names := c.Names
	if len(names) == 0 {
		names = DefaultTypes
	}
	return slices.Contains(names, typ)
}
