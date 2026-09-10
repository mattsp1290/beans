package issue

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// HubConfig is ~/.beans/hub/beans.toml.
type HubConfig struct {
	Workflow WorkflowFile `toml:"workflow"`
	Types    TypesConfig  `toml:"types"`
	IDs      IDsConfig    `toml:"ids"`
}

// IDsConfig is the [ids] table of the hub beans.toml.
type IDsConfig struct {
	Length int `toml:"length"`
}

// ProjectConfig is projects/<name>/beans.toml.
type ProjectConfig struct {
	Name     string       `toml:"name"`
	Prefix   string       `toml:"prefix"`
	Remotes  []string     `toml:"remotes"`
	Workflow WorkflowFile `toml:"workflow"`
}

// UserConfig is ~/.beans/config.toml. It never lives in the hub.
type UserConfig struct {
	Actor string          `toml:"actor"`
	Hub   UserHubConfig   `toml:"hub"`
	Fetch UserFetchConfig `toml:"fetch"`
}

// UserHubConfig is the [hub] table of the user config.
type UserHubConfig struct {
	Remote string `toml:"remote"`
	Branch string `toml:"branch"`
}

// UserFetchConfig is the [fetch] table of the user config.
type UserFetchConfig struct {
	Throttle string `toml:"throttle"`
}

// DefaultFetchThrottle applies when [fetch] throttle is unset.
const DefaultFetchThrottle = 60 * time.Second

// ThrottleDuration parses the throttle, falling back to the default.
func (c UserConfig) ThrottleDuration() time.Duration {
	if strings.TrimSpace(c.Fetch.Throttle) == "" {
		return DefaultFetchThrottle
	}
	d, err := time.ParseDuration(strings.TrimSpace(c.Fetch.Throttle))
	if err != nil || d < 0 {
		return DefaultFetchThrottle
	}
	return d
}

// LoadHubConfig reads path. A missing file yields defaults.
func LoadHubConfig(path string) (HubConfig, error) {
	cfg := HubConfig{Types: DefaultTypesConfig(), IDs: IDsConfig{Length: DefaultIDLength}}
	if err := decodeTOMLFile(path, &cfg); err != nil {
		return HubConfig{}, err
	}
	if cfg.IDs.Length <= 0 {
		cfg.IDs.Length = DefaultIDLength
	}
	if len(cfg.Types.Names) == 0 {
		cfg.Types = DefaultTypesConfig()
	}
	return cfg, nil
}

// LoadProjectConfig reads path. A missing file yields an empty config.
func LoadProjectConfig(path string) (ProjectConfig, error) {
	var cfg ProjectConfig
	if err := decodeTOMLFile(path, &cfg); err != nil {
		return ProjectConfig{}, err
	}
	return cfg, nil
}

// LoadUserConfig reads path. A missing file yields defaults.
func LoadUserConfig(path string) (UserConfig, error) {
	var cfg UserConfig
	if err := decodeTOMLFile(path, &cfg); err != nil {
		return UserConfig{}, err
	}
	return cfg, nil
}

// EncodeUserConfig renders the user config as TOML.
func EncodeUserConfig(cfg UserConfig) ([]byte, error) {
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(cfg); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// EncodeProjectConfig renders a project config as TOML. An empty [workflow]
// override is omitted.
func EncodeProjectConfig(cfg ProjectConfig) ([]byte, error) {
	type projectTOML struct {
		Name     string        `toml:"name"`
		Prefix   string        `toml:"prefix"`
		Remotes  []string      `toml:"remotes"`
		Workflow *WorkflowFile `toml:"workflow,omitempty"`
	}
	out := projectTOML{Name: cfg.Name, Prefix: cfg.Prefix, Remotes: cfg.Remotes}
	if out.Remotes == nil {
		out.Remotes = []string{}
	}
	if !cfg.Workflow.IsEmpty() {
		wf := cfg.Workflow
		out.Workflow = &wf
	}
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(out); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// decodeTOMLBytes decodes TOML data into dst with the same error shape as
// decodeTOMLFile, without a file name.
func decodeTOMLBytes(data []byte, dst any) error {
	if _, err := toml.Decode(string(data), dst); err != nil {
		return err
	}
	return nil
}

// decodeTOMLFile decodes path into dst. A missing file is not an error. A
// malformed file produces an error naming the file and line.
func decodeTOMLFile(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%s: %w", path, err)
	}
	if _, err := toml.Decode(string(data), dst); err != nil {
		var perr toml.ParseError
		if errors.As(err, &perr) {
			return fmt.Errorf("%s: line %d: %s", path, perr.Position.Line, perr.Message)
		}
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
