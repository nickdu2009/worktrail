package daemon

import (
	"errors"
	"path/filepath"
	"strings"
)

func validateLockRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return errors.New("semantic daemon lock root must be a clean absolute path")
	}
	return nil
}

func validateBundleID(bundleID string) error {
	if bundleID == "" ||
		strings.TrimSpace(bundleID) != bundleID ||
		bundleID == "." ||
		bundleID == ".." ||
		filepath.Base(bundleID) != bundleID ||
		strings.ContainsAny(bundleID, `/\`) {
		return errors.New("semantic daemon bundle ID is invalid")
	}
	return nil
}
