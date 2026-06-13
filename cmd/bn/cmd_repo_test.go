package main

import "testing"

func TestCleanRepoSlug(t *testing.T) {
	t.Parallel()

	for _, slug := range []string{"boxy", "birbparty.clckr", "shady-api", "repo_1"} {
		got, err := cleanRepoSlug(slug)
		if err != nil {
			t.Fatalf("cleanRepoSlug(%q): %v", slug, err)
		}
		if got != slug {
			t.Fatalf("cleanRepoSlug(%q) = %q, want same", slug, got)
		}
	}

	for _, slug := range []string{"", "Boxy", " boxy", "boxy ", "-boxy", "boxy repo"} {
		if _, err := cleanRepoSlug(slug); err == nil {
			t.Fatalf("cleanRepoSlug(%q): want error", slug)
		}
	}
}

func TestRootCommandIncludesRepoCommand(t *testing.T) {
	t.Parallel()

	root := newRootCmd(&appState{})
	for _, cmd := range root.Commands() {
		if cmd.Name() == "repo" {
			return
		}
	}
	t.Fatal("root command missing repo subcommand")
}

func TestRepoCommandIncludesDoctor(t *testing.T) {
	t.Parallel()

	repoCmd := newRepoCmd(&appState{})
	for _, cmd := range repoCmd.Commands() {
		if cmd.Name() == "doctor" {
			if cmd.Flags().Lookup("from-orchestrator") == nil {
				t.Fatal("repo doctor missing --from-orchestrator flag")
			}
			if cmd.Flags().Lookup("allowed-host") == nil {
				t.Fatal("repo doctor missing --allowed-host flag")
			}
			return
		}
	}
	t.Fatal("repo command missing doctor subcommand")
}

func TestUpdateCommandIncludesRepoRoutingFlags(t *testing.T) {
	t.Parallel()

	cmd := newUpdateCmd(&appState{})
	for _, name := range []string{"repo", "ref", "subdir", "force"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("update command missing --%s flag", name)
		}
	}
}
