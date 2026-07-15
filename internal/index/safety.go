package index

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validateIndexPath(root, target string) error {
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
		return fmt.Errorf("index path %q escapes root %q", target, root)
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
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("index path %q is a symbolic link", current)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("index path component %q is not a directory", current)
		}
	}
	return nil
}

func ensureIndexOutputDir(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := validateIndexPath(root, root); err != nil {
		return "", err
	}
	indexDir := filepath.Join(root, "index")
	info, err := os.Lstat(indexDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.Mkdir(indexDir, 0o755); err != nil {
			return "", err
		}
		info, err = os.Lstat(indexDir)
		if err != nil {
			return "", err
		}
	case err != nil:
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("index output directory %q is a symbolic link", indexDir)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("index output path %q is not a directory", indexDir)
	}
	return indexDir, nil
}

func indexArtifactStatus(root, name string) (bool, os.FileInfo, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return false, nil, err
	}
	indexDir := filepath.Join(root, "index")
	if err := validateIndexPath(root, indexDir); errors.Is(err, os.ErrNotExist) {
		return false, nil, nil
	} else if err != nil {
		return false, nil, err
	}
	path := filepath.Join(indexDir, name)
	if err := validateIndexPath(root, path); errors.Is(err, os.ErrNotExist) {
		return false, nil, nil
	} else if err != nil {
		return false, nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false, nil, err
	}
	if !info.Mode().IsRegular() {
		return false, nil, fmt.Errorf("index artifact %q is not a regular file", path)
	}
	return true, info, nil
}

func readIndexRegularFile(root, path string) ([]byte, os.FileInfo, error) {
	if err := validateIndexPath(root, path); err != nil {
		return nil, nil, err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("index source %q is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, nil, fmt.Errorf("index source %q changed while reading", path)
	}
	return data, after, nil
}

func removeIndexArtifact(root, name string) error {
	exists, _, err := indexArtifactStatus(root, name)
	if err != nil || !exists {
		return err
	}
	return os.Remove(filepath.Join(root, "index", name))
}
