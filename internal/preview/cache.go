package preview

import (
	"os"

	"github.com/nickdu2009/worktrail/internal/paths"
)

func CacheDir(env paths.Env, scope string) (string, error) {
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return "", err
	}
	return paths.SafeJoin(root, ".cache", "preview")
}

func ClearCache(env paths.Env, scope string) (string, error) {
	dir, err := CacheDir(env, scope)
	if err != nil {
		return "", err
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}
