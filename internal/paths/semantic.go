package paths

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// SemanticRoots keeps evictable bundles, mutable runtime state, and logs out
// of the formal WORKTRAIL_HOME knowledge root.
type SemanticRoots struct {
	Cache   string
	Runtime string
	Logs    string
}

func DiscoverSemanticRoots() (SemanticRoots, error) {
	cacheBase, err := os.UserCacheDir()
	if err != nil {
		return SemanticRoots{}, err
	}
	configBase, err := os.UserConfigDir()
	if err != nil {
		return SemanticRoots{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return SemanticRoots{}, err
	}
	return SemanticRoots{
		Cache:   filepath.Join(cacheBase, "worktrail", "semantic"),
		Runtime: filepath.Join(configBase, "worktrail", "semantic"),
		Logs:    filepath.Join(home, "Library", "Logs", "worktrail", "semantic"),
	}, nil
}

func (r SemanticRoots) Bundle(bundleID string) (string, error) {
	if err := validateBundleID(bundleID); err != nil {
		return "", err
	}
	return SafeJoin(filepath.Join(r.Cache, "bundles"), bundleID)
}

func (r SemanticRoots) RuntimeState(bundleID string) (string, error) {
	if err := validateBundleID(bundleID); err != nil {
		return "", err
	}
	return SafeJoin(r.Runtime, bundleID)
}

func (r SemanticRoots) Log(bundleID string) (string, error) {
	if err := validateBundleID(bundleID); err != nil {
		return "", err
	}
	return SafeJoin(r.Logs, bundleID+".log")
}

func validateBundleID(bundleID string) error {
	if bundleID == "" || strings.TrimSpace(bundleID) != bundleID ||
		filepath.Base(bundleID) != bundleID || bundleID == "." || bundleID == ".." {
		return errors.New("invalid semantic bundle ID")
	}
	return nil
}
