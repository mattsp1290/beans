package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandWithoutArgsPrintsHelp(t *testing.T) {
	root := newRootCmd(&appState{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(nil)
	if err := root.Execute(); err != nil {
		t.Fatalf("bn with no args: %v", err)
	}
	if !strings.Contains(out.String(), "Git-backed issue tracker") {
		t.Errorf("help output missing short description:\n%s", out.String())
	}
}

func TestResolveActorPrecedence(t *testing.T) {
	t.Setenv("BN_ACTOR", "env-actor")
	if got := (&appState{actor: "flag-actor"}).resolveActor(); got != "flag-actor" {
		t.Errorf("--actor should win, got %q", got)
	}
	if got := (&appState{}).resolveActor(); got != "env-actor" {
		t.Errorf("BN_ACTOR should win over git config, got %q", got)
	}
}

func TestWriteJSONTo(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := writeJSONTo(&out, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\n  \"a\": 1\n}\n" {
		t.Errorf("unexpected JSON: %q", out.String())
	}
}
