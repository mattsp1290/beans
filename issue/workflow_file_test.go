package issue

import "testing"

func TestDecodeWorkflowFileTOMLAndYAML(t *testing.T) {
	t.Parallel()

	fromTOML, err := decodeWorkflowFile("beans.toml", []byte(`
[workflow]
statuses = ["open", "in_progress", "qa", "closed"]
default = "open"
active = ["open"]
terminal = ["closed"]
`))
	if err != nil {
		t.Fatalf("decode TOML: %v", err)
	}
	fromYAML, err := decodeWorkflowFile("wf.yaml", []byte(`
workflow:
  statuses: [open, in_progress, qa, closed]
  default: open
  active: [open]
  terminal: [closed]
`))
	if err != nil {
		t.Fatalf("decode YAML: %v", err)
	}

	wt := mergeWorkflowFile(DefaultWorkflowConfig(), fromTOML)
	wy := mergeWorkflowFile(DefaultWorkflowConfig(), fromYAML)
	if len(wt.Statuses) != 4 || !wt.IsValid("qa") || wt.IsValid("done") || !wt.IsTerminal("closed") {
		t.Errorf("TOML config wrong: %+v", wt)
	}
	if len(wy.Statuses) != len(wt.Statuses) || !wy.IsValid("qa") {
		t.Errorf("YAML config should match TOML: %+v vs %+v", wy, wt)
	}
}

func TestMergeWorkflowFilePartialInheritsBase(t *testing.T) {
	t.Parallel()
	file, err := decodeWorkflowFile("beans.toml", []byte("[workflow]\ndefault = \"in_progress\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	wf := mergeWorkflowFile(DefaultWorkflowConfig(), file)
	if wf.DefaultState() != "in_progress" {
		t.Errorf("default = %q, want in_progress", wf.DefaultState())
	}
	if !wf.IsValid("ready_for_review") || !wf.IsTerminal("done") {
		t.Errorf("partial config should inherit base vocabulary: %+v", wf)
	}
}

func TestDecodeWorkflowFileUnknownKeysFailFast(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, file, body string }{
		{"toml", "bad.toml", "[workflow]\ndefaultx = \"open\"\n"},
		{"yaml", "bad.yaml", "workflow:\n  defaultx: open\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeWorkflowFile(tt.file, []byte(tt.body)); err == nil {
				t.Fatal("expected error for unknown workflow config key")
			}
		})
	}
}

func TestDecodeWorkflowFileIgnoresOtherTOMLTables(t *testing.T) {
	t.Parallel()
	file, err := decodeWorkflowFile("beans.toml", []byte("[workflow]\ndefault = \"open\"\n\n[types]\nnames = [\"task\"]\n"))
	if err != nil {
		t.Fatalf("other tables must not be unknown keys: %v", err)
	}
	if file.Workflow.Default != "open" {
		t.Errorf("default = %q, want open", file.Workflow.Default)
	}
}

func TestDecodeWorkflowFileUnsupportedExtension(t *testing.T) {
	t.Parallel()
	if _, err := decodeWorkflowFile("wf.json", []byte("{}")); err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestMergedInvalidBucketFailsValidate(t *testing.T) {
	t.Parallel()
	file, err := decodeWorkflowFile("bad.toml", []byte(`
[workflow]
statuses = ["open", "closed"]
default = "open"
active = ["ghost"]
terminal = ["closed"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := mergeWorkflowFile(DefaultWorkflowConfig(), file).Validate(); err == nil {
		t.Fatal("expected validation error for active status not in statuses")
	}
}
