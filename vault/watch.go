package vault

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounceInterval is how long Watch waits after the last relevant event
// before reloading and calling onChange.
const debounceInterval = 200 * time.Millisecond

// Watch watches the hub directory tree rooted at ix.HubDir for markdown
// changes, recursively, excluding .git and dot-directories (and templates/).
// Create/write/rename/remove events on .md files are debounced by 200ms; when
// the debounce fires, Watch calls ix.Reload with the affected hub-relative
// paths, then onChange with the same paths. Directories created later are
// added to the watch set. Watch returns when ctx is done.
func Watch(ctx context.Context, ix *Index, onChange func(paths []string)) error {
	return WatchWithOptions(ctx, ix, WatchOptions{OnChange: onChange})
}

// WatchOptions configures WatchWithOptions.
type WatchOptions struct {
	OnChange func(paths []string)
	// OnError receives a Reload failure (an I/O error or a broken beans.toml);
	// nil ignores them. Parse problems are Warnings on the index, not errors.
	OnError func(err error)
}

// WatchWithOptions is Watch with an error callback. beans.toml edits are
// forwarded too and trigger a full reload.
func WatchWithOptions(ctx context.Context, ix *Index, opts WatchOptions) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	if err := addWatchDirs(w, ix.HubDir); err != nil {
		return err
	}

	pending := map[string]bool{}
	var timer *time.Timer
	var timerC <-chan time.Time

	resetTimer := func() {
		if timer == nil {
			timer = time.NewTimer(debounceInterval)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounceInterval)
		}
		timerC = timer.C
	}

	flush := func() {
		if len(pending) == 0 {
			return
		}
		paths := make([]string, 0, len(pending))
		for p := range pending {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		pending = map[string]bool{}
		if err := ix.Reload(paths...); err != nil && opts.OnError != nil {
			opts.OnError(err)
		}
		if opts.OnChange != nil {
			opts.OnChange(paths)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if handleEvent(w, ix.HubDir, ev, pending) {
				resetTimer()
			}
		case <-timerC:
			timerC = nil
			flush()
		case _, ok := <-w.Errors:
			if !ok {
				return nil
			}
		}
	}
}

// handleEvent processes one fsnotify event: it adds newly created
// directories to the watch set, and records relevant .md file paths into
// pending. It reports whether pending changed.
func handleEvent(w *fsnotify.Watcher, hubDir string, ev fsnotify.Event, pending map[string]bool) bool {
	if ev.Op&fsnotify.Create != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			if skipDirName(filepath.Base(ev.Name)) {
				return false
			}
			_ = addWatchDirs(w, ev.Name)
			// Files written between the directory's creation and the watch
			// being added produced no events; pick them up now.
			changed := false
			_ = filepath.WalkDir(ev.Name, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if rel, ok := watchedRel(hubDir, path); ok {
					pending[rel] = true
					changed = true
				}
				return nil
			})
			return changed
		}
	}
	if _, ok := watchedRel(hubDir, ev.Name); !ok {
		return false
	}
	if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) == 0 {
		return false
	}
	rel, _ := watchedRel(hubDir, ev.Name)
	pending[rel] = true
	return true
}

// watchedRel returns the hub-relative path of a file the watcher cares
// about: any .md file, or a beans.toml, outside ignored directories.
func watchedRel(hubDir, name string) (string, bool) {
	base := filepath.Base(name)
	if strings.ToLower(filepath.Ext(name)) != ".md" && base != "beans.toml" {
		return "", false
	}
	rel, err := filepath.Rel(hubDir, name)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if pathIgnored(rel) {
		return "", false
	}
	return rel, true
}

// addWatchDirs adds root and every non-dot, non-templates subdirectory under
// it to w.
func addWatchDirs(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipDirName(d.Name()) {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}

func pathIgnored(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") || seg == "templates" {
			return true
		}
	}
	return false
}
