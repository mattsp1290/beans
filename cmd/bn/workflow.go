package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"github.com/mattsp1290/beans/model"
)

// activeWorkflow is the process-wide status vocabulary and bucket
// classification. It defaults to the built-in config and is replaced once a
// deployment config file is loaded (see appState.ensureWorkflow). Free CLI
// helpers (status validation, import filtering, ready/terminal sets) consult it,
// mirroring the package-global style of the status sets it replaced.
var activeWorkflow = model.DefaultWorkflowConfig()

// workflowEnv is the env var pointing at an explicit config file path.
const workflowEnv = "BN_CONFIG"

// workflowFile is the on-disk schema for the [workflow] section, decoded from
// TOML or YAML. All fields are optional; omitted fields inherit the built-in
// default (key-level merge, not deep).
type workflowFile struct {
	Workflow struct {
		Statuses    []string            `toml:"statuses" yaml:"statuses"`
		Default     string              `toml:"default" yaml:"default"`
		Active      []string            `toml:"active" yaml:"active"`
		Terminal    []string            `toml:"terminal" yaml:"terminal"`
		Transitions map[string][]string `toml:"transitions" yaml:"transitions"`
	} `toml:"workflow" yaml:"workflow"`
}

// ensureWorkflow loads the workflow config once and caches it on the appState
// and in the activeWorkflow global. It fails fast on an invalid config so a
// deployment mistake stops bn at startup rather than silently falling back.
func (rs *appState) ensureWorkflow() error {
	if rs.workflowLoaded {
		return nil
	}
	wf, err := loadWorkflowConfig()
	if err != nil {
		return err
	}
	rs.workflow = wf
	rs.workflowLoaded = true
	activeWorkflow = wf
	return nil
}

// loadWorkflowConfig resolves, decodes, merges, and validates the workflow
// config. With no config file found it returns the built-in default.
func loadWorkflowConfig() (model.WorkflowConfig, error) {
	path, explicit, err := resolveWorkflowConfigPath()
	if err != nil {
		return model.WorkflowConfig{}, err
	}
	if path == "" {
		return model.DefaultWorkflowConfig(), nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if explicit {
			return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: %w", path, err)
		}
		// A discovered (non-explicit) file that vanished between stat and read:
		// fall back to defaults rather than failing.
		return model.DefaultWorkflowConfig(), nil
	}

	var file workflowFile
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".toml":
		if err := toml.Unmarshal(raw, &file); err != nil {
			return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: %w", path, err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(raw, &file); err != nil {
			return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: %w", path, err)
		}
	default:
		return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: unsupported extension %q (use .toml, .yaml, or .yml)", path, ext)
	}

	wf := mergeWorkflowFile(model.DefaultWorkflowConfig(), file)
	if err := wf.Validate(); err != nil {
		return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: %w", path, err)
	}
	return wf, nil
}

// mergeWorkflowFile overlays the decoded file onto base. A field present in the
// file (non-empty) replaces the base field wholesale; absent fields are
// inherited. This gives partial configs (e.g. set only `default`) sensible
// defaults for everything else.
func mergeWorkflowFile(base model.WorkflowConfig, file workflowFile) model.WorkflowConfig {
	w := file.Workflow
	if len(w.Statuses) > 0 {
		base.Statuses = toStates(w.Statuses)
	}
	if strings.TrimSpace(w.Default) != "" {
		base.Default = model.IssueState(strings.TrimSpace(w.Default))
	}
	if len(w.Active) > 0 {
		base.Active = toStates(w.Active)
	}
	if len(w.Terminal) > 0 {
		base.Terminal = toStates(w.Terminal)
	}
	if len(w.Transitions) > 0 {
		trans := make(map[model.IssueState][]model.IssueState, len(w.Transitions))
		for from, tos := range w.Transitions {
			trans[model.IssueState(from)] = toStates(tos)
		}
		base.Transitions = trans
	}
	return base
}

func toStates(in []string) []model.IssueState {
	out := make([]model.IssueState, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, model.IssueState(s))
	}
	return out
}

// resolveWorkflowConfigPath finds the config file to load. It returns the path,
// whether it was set explicitly (via BN_CONFIG, in which case an unreadable file
// is a hard error), and any discovery error. An empty path means "use defaults".
//
// Precedence:
//  1. BN_CONFIG env var (explicit; error if set but missing/unreadable).
//  2. Walk up from the working directory for bn.toml / bn.yaml / bn.yml.
//  3. $XDG_CONFIG_HOME/bn/config.{toml,yaml,yml} (or ~/.config/bn/...).
func resolveWorkflowConfigPath() (path string, explicit bool, err error) {
	if env := strings.TrimSpace(os.Getenv(workflowEnv)); env != "" {
		info, statErr := os.Stat(env)
		if statErr != nil {
			return "", true, fmt.Errorf("%s=%s: %w", workflowEnv, env, statErr)
		}
		if info.IsDir() {
			return "", true, fmt.Errorf("%s=%s is a directory; expected a config file", workflowEnv, env)
		}
		return env, true, nil
	}

	if p, found, walkErr := walkUpForWorkflowConfig(); walkErr != nil {
		return "", false, walkErr
	} else if found {
		return p, false, nil
	}

	if p, found := xdgWorkflowConfig(); found {
		return p, false, nil
	}

	return "", false, nil
}

// workflowConfigNames are the discovery filenames in preference order within a
// single directory (TOML preferred, then YAML).
var workflowConfigNames = []string{"bn.toml", "bn.yaml", "bn.yml"}

func walkUpForWorkflowConfig() (string, bool, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("workflow config: get working directory: %w", err)
	}
	dir, err := filepath.Abs(wd)
	if err != nil {
		return "", false, fmt.Errorf("workflow config: resolve %s: %w", wd, err)
	}
	for {
		for _, name := range workflowConfigNames {
			p := filepath.Join(dir, name)
			if info, statErr := os.Stat(p); statErr == nil && !info.IsDir() {
				return p, true, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

func xdgWorkflowConfig() (string, bool) {
	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", false
		}
		base = filepath.Join(home, ".config")
	}
	for _, name := range []string{"config.toml", "config.yaml", "config.yml"} {
		p := filepath.Join(base, "bn", name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, true
		}
	}
	return "", false
}
