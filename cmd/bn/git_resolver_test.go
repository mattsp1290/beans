package main

// compile-time interface satisfaction check for the test double
var _ gitResolver = (*fakeGitResolver)(nil)

// fakeGitResolver is a test double that returns predetermined values without
// touching the filesystem or spawning processes.
type fakeGitResolver struct {
	toplevel  string
	remoteURL string
	// err values are returned for every call when non-nil
	toplevelErr  error
	remoteURLErr error
	// lastToplevelDir and lastRemoteURLRoot record the arguments of the most
	// recent call so tests can assert correct wiring (e.g. RemoteURL receives
	// the root returned by Toplevel, not cwd).
	lastToplevelDir   string
	lastRemoteURLRoot string
}

func (f *fakeGitResolver) Toplevel(dir string) (string, bool, error) {
	f.lastToplevelDir = dir
	if f.toplevelErr != nil {
		return "", false, f.toplevelErr
	}
	if f.toplevel == "" {
		return "", false, nil
	}
	return f.toplevel, true, nil
}

func (f *fakeGitResolver) RemoteURL(root string) (string, bool, error) {
	f.lastRemoteURLRoot = root
	if f.remoteURLErr != nil {
		return "", false, f.remoteURLErr
	}
	if f.remoteURL == "" {
		return "", false, nil
	}
	return f.remoteURL, true, nil
}
