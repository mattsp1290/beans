package gitops

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ReplaceTree atomically installs files as target's complete directory tree
// on a single filesystem. Files must be slash-relative regular-file names.
// A caller that is interrupted before cleanup leaves a complete backup rather
// than a mixture of old and new bundle files.
func ReplaceTree(target string, files map[string][]byte) error {
	parent, base, err := treeParts(target)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if err := RecoverTree(target); err != nil {
		return err
	}
	backup := filepath.Join(parent, "."+base+".backup")
	stage, err := os.MkdirTemp(parent, "."+base+".stage-")
	if err != nil {
		return err
	}
	keepStage := true
	defer func() {
		if keepStage {
			_ = os.RemoveAll(stage)
		}
	}()
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	for _, name := range paths {
		if name == "" || filepath.IsAbs(name) || strings.Contains(name, "\\") || strings.HasPrefix(filepath.Clean(name), "..") {
			return fmt.Errorf("unsafe tree file %q", name)
		}
		out := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, files[name], 0o644); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf("tree backup already exists: %s", backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	_, oldErr := os.Lstat(target)
	if oldErr == nil {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	} else if !os.IsNotExist(oldErr) {
		return oldErr
	}
	if err := os.Rename(stage, target); err != nil {
		if oldErr == nil {
			_ = os.Rename(backup, target)
		}
		return err
	}
	keepStage = false
	if oldErr == nil {
		if err := os.RemoveAll(backup); err != nil {
			return err
		}
	}
	return nil
}

// RecoverTree restores an interrupted tree replacement before a reader uses
// target. If target is already present, any backup is from a completed
// replacement and can be discarded.
func RecoverTree(target string) error {
	parent, base, err := treeParts(target)
	if err != nil {
		return err
	}
	backup := filepath.Join(parent, "."+base+".backup")
	if _, err := os.Lstat(backup); err == nil {
		if _, targetErr := os.Lstat(target); os.IsNotExist(targetErr) {
			if err := os.Rename(backup, target); err != nil {
				return fmt.Errorf("recover tree backup: %w", err)
			}
		} else if targetErr == nil {
			if err := os.RemoveAll(backup); err != nil {
				return fmt.Errorf("clear completed tree backup: %w", err)
			}
		} else {
			return targetErr
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RecoverTrees repairs interrupted replacements below parent. It is intended
// for readers that discover a plans directory rather than a missing bundle.
func RecoverTrees(parent string) error {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".backup") {
			continue
		}
		base := strings.TrimSuffix(strings.TrimPrefix(name, "."), ".backup")
		if base == "" || base == "." || base == string(filepath.Separator) {
			continue
		}
		if err := RecoverTree(filepath.Join(parent, base)); err != nil {
			return err
		}
	}
	return nil
}

func treeParts(target string) (string, string, error) {
	parent, base := filepath.Dir(target), filepath.Base(target)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "", "", fmt.Errorf("invalid tree target %q", target)
	}
	return parent, base, nil
}
