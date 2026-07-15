package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsurePrivateDir creates a directory below root one component at a time.
// Existing path components are inspected with Lstat so symbolic links cannot
// redirect runtime writes outside the Worktrail root.
func EnsurePrivateDir(root, rel string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("runtime root is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := ensureRootDir(root); err != nil {
		return "", err
	}
	rel = filepath.Clean(strings.TrimSpace(rel))
	if rel == "." || rel == "" {
		return root, nil
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("runtime directory %q escapes root", rel)
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		switch {
		case errors.Is(statErr, os.ErrNotExist):
			if err := os.Mkdir(current, 0o700); err != nil {
				return "", err
			}
			info, statErr = os.Lstat(current)
			if statErr != nil {
				return "", statErr
			}
		case statErr != nil:
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("runtime path %q is a symbolic link", current)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("runtime path %q is not a directory", current)
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return "", err
		}
	}
	return current, nil
}

func ensureRootDir(root string) error {
	info, err := os.Lstat(root)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(root, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(root)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("runtime root %q is a symbolic link", root)
	}
	if !info.IsDir() {
		return fmt.Errorf("runtime root %q is not a directory", root)
	}
	return nil
}

func rejectSymlinkPath(root, target string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("runtime path %q escapes root %q", target, root)
	}
	current := root
	parts := []string{}
	if rel != "." {
		parts = strings.Split(rel, string(filepath.Separator))
	}
	for index := -1; index < len(parts); index++ {
		if index >= 0 {
			current = filepath.Join(current, parts[index])
		}
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("runtime path %q is a symbolic link", current)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("runtime path component %q is not a directory", current)
		}
	}
	return nil
}

func lstatRegularFile(root, path string) (os.FileInfo, error) {
	if err := rejectSymlinkPath(root, path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("runtime path %q is not a regular file", path)
	}
	return info, nil
}

func readRegularFile(root, path string) ([]byte, os.FileInfo, error) {
	before, err := lstatRegularFile(root, path)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	after, err := lstatRegularFile(root, path)
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(before, after) {
		return nil, nil, fmt.Errorf("runtime path %q changed while reading", path)
	}
	return data, after, nil
}
