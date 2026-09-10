package version

import "testing"

func TestVersionNotEmpty(t *testing.T) {
	t.Parallel()
	if Version == "" {
		t.Fatal("Version must not be empty; default should be 'dev' or an ldflag-injected value")
	}
}
