package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"github.com/mattsp1290/beans/model"
)

// workflowEnv is the env var pointing at an explicit config file path.
const workflowEnv = "BN_CONFIG"

// workflowDefaultEnv overrides just the default new-issue status after any file
// config has been merged.
const workflowDefaultEnv = "BN_STATUS_DEFAULT"

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

// ensureWorkflow loads the workflow config once and caches it on the appState.
// It fails fast on an invalid config so a
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
	return nil
}

func (rs *appState) workflowConfig() model.WorkflowConfig {
	if len(rs.workflow.Statuses) != 0 {
		return rs.workflow
	}
	if rs.store != nil {
		return rs.store.WorkflowConfig()
	}
	return model.DefaultWorkflowConfig()
}

// loadWorkflowConfig resolves, decodes, merges, and validates the workflow
// config. With no config file found it returns the built-in default.
func loadWorkflowConfig() (model.WorkflowConfig, error) {
	wf := model.DefaultWorkflowConfig()
	path, explicit, err := resolveWorkflowConfigPath()
	if err != nil {
		return model.WorkflowConfig{}, err
	}

	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			if explicit {
				return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: %w", path, err)
			}
			// A discovered (non-explicit) file that vanished between stat and read
			// falls back to defaults; other read failures indicate a real bad
			// deployment config and must fail fast.
			if !os.IsNotExist(err) {
				return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: %w", path, err)
			}
		} else {
			var file workflowFile
			switch ext := strings.ToLower(filepath.Ext(path)); ext {
			case ".toml":
				meta, err := toml.Decode(string(raw), &file)
				if err != nil {
					return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: %w", path, err)
				}
				if undecoded := meta.Undecoded(); len(undecoded) > 0 {
					return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: unknown key(s): %s", path, joinTOMLKeys(undecoded))
				}
			case ".yaml", ".yml":
				dec := yaml.NewDecoder(bytes.NewReader(raw))
				dec.KnownFields(true)
				if err := dec.Decode(&file); err != nil {
					return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: %w", path, err)
				}
			default:
				return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: unsupported extension %q (use .toml, .yaml, or .yml)", path, ext)
			}
			wf = mergeWorkflowFile(wf, file)
		}
	}

	if v := strings.TrimSpace(os.Getenv(workflowDefaultEnv)); v != "" {
		wf.Default = model.IssueState(v)
	}

	if err := wf.Validate(); err != nil {
		if path != "" {
			return model.WorkflowConfig{}, fmt.Errorf("workflow config %s: %w", path, err)
		}
		return model.WorkflowConfig{}, err
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

func joinTOMLKeys(keys []toml.Key) string {
	out := make([]string, len(keys))
	for i, key := range keys {
		out[i] = key.String()
	}
	return strings.Join(out, ", ")
}

// resolveWorkflowConfigPath finds the config file to load. It returns the path,
// whether it was set explicitly (via BN_CONFIG, in which case an unreadable file
// is a hard error), and any discovery error. An empty path means "use defaults".
//
// Precedence:
//  1. BN_CONFIG env var (explicit; error if set but missing/unreadable).
//  2. bn.toml / bn.yaml / bn.yml in the working directory.
//  3. bn.toml / bn.yaml / bn.yml next to the discovered .bn marker.
//  4. $XDG_CONFIG_HOME/bn/config.{toml,yaml,yml} (or ~/.config/bn/...).
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

	if p, found, cwdErr := workflowConfigInDir(""); cwdErr != nil {
		return "", false, cwdErr
	} else if found {
		return p, false, nil
	}

	markerPath, markerFound, err := activeProjectMarkerPath("")
	if err != nil {
		return "", false, fmt.Errorf("workflow config: %w", err)
	}
	if markerFound {
		if p, found, markerErr := workflowConfigInDir(filepath.Dir(markerPath)); markerErr != nil {
			return "", false, markerErr
		} else if found {
			return p, false, nil
		}
	}

	if p, found := xdgWorkflowConfig(); found {
		return p, false, nil
	}

	return "", false, nil
}

// workflowConfigNames are the discovery filenames in preference order within a
// single directory (TOML preferred, then YAML).
var workflowConfigNames = []string{"bn.toml", "bn.yaml", "bn.yml"}

func workflowConfigInDir(dir string) (string, bool, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", false, fmt.Errorf("workflow config: get working directory: %w", err)
		}
		dir = wd
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false, fmt.Errorf("workflow config: resolve %s: %w", dir, err)
	}
	for _, name := range workflowConfigNames {
		p := filepath.Join(dir, name)
		info, statErr := os.Stat(p)
		switch {
		case statErr == nil && !info.IsDir():
			return p, true, nil
		case statErr == nil:
			return "", false, fmt.Errorf("%s is a directory; expected workflow config file", p)
		case !os.IsNotExist(statErr):
			return "", false, fmt.Errorf("stat %s: %w", p, statErr)
		}
	}
	return "", false, nil
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
