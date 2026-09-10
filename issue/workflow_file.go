package issue

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// workflowFile is the on-disk schema for the [workflow] section, decoded from
// TOML or YAML. All fields are optional; omitted fields inherit the base
// config they are merged onto (key-level merge, not deep).
type workflowFile struct {
	Workflow struct {
		Statuses    []string            `toml:"statuses" yaml:"statuses"`
		Default     string              `toml:"default" yaml:"default"`
		Active      []string            `toml:"active" yaml:"active"`
		Terminal    []string            `toml:"terminal" yaml:"terminal"`
		Transitions map[string][]string `toml:"transitions" yaml:"transitions"`
	} `toml:"workflow" yaml:"workflow"`
}

// decodeWorkflowFile decodes raw as TOML or YAML based on the extension of
// name. Unknown keys under [workflow] are an error; keys outside [workflow]
// are ignored so a beans.toml that also carries [types] or [ids] decodes.
func decodeWorkflowFile(name string, raw []byte) (workflowFile, error) {
	var file workflowFile
	switch ext := strings.ToLower(filepath.Ext(name)); ext {
	case ".toml":
		meta, err := toml.Decode(string(raw), &file)
		if err != nil {
			return workflowFile{}, fmt.Errorf("workflow config %s: %w", name, err)
		}
		if undecoded := undecodedWorkflowKeys(meta); len(undecoded) > 0 {
			return workflowFile{}, fmt.Errorf("workflow config %s: unknown key(s): %s", name, strings.Join(undecoded, ", "))
		}
	case ".yaml", ".yml":
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&file); err != nil {
			return workflowFile{}, fmt.Errorf("workflow config %s: %w", name, err)
		}
	default:
		return workflowFile{}, fmt.Errorf("workflow config %s: unsupported extension %q (use .toml, .yaml, or .yml)", name, ext)
	}
	return file, nil
}

// undecodedWorkflowKeys lists undecoded TOML keys that live under [workflow].
// Other top-level tables are legitimately owned by other decoders.
func undecodedWorkflowKeys(meta toml.MetaData) []string {
	var out []string
	for _, key := range meta.Undecoded() {
		if len(key) > 0 && key[0] == "workflow" {
			out = append(out, key.String())
		}
	}
	return out
}

// mergeWorkflowFile overlays the decoded file onto base. A field present in the
// file (non-empty) replaces the base field wholesale; absent fields are
// inherited. This gives partial configs (e.g. set only `default`) sensible
// defaults for everything else.
func mergeWorkflowFile(base WorkflowConfig, file workflowFile) WorkflowConfig {
	w := file.Workflow
	if len(w.Statuses) > 0 {
		base.Statuses = cleanStates(w.Statuses)
	}
	if strings.TrimSpace(w.Default) != "" {
		base.Default = strings.TrimSpace(w.Default)
	}
	if len(w.Active) > 0 {
		base.Active = cleanStates(w.Active)
	}
	if len(w.Terminal) > 0 {
		base.Terminal = cleanStates(w.Terminal)
	}
	if len(w.Transitions) > 0 {
		trans := make(map[string][]string, len(w.Transitions))
		for from, tos := range w.Transitions {
			trans[from] = cleanStates(tos)
		}
		base.Transitions = trans
	}
	return base
}

func cleanStates(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
