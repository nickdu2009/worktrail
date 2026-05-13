package paths

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Env struct {
	Home        string
	UserRoot    string
	ProjectRoot string
	ProjectWT   string
}

func Discover() (Env, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	userRoot := os.Getenv("WORKTRAIL_HOME")
	if userRoot == "" {
		userRoot = filepath.Join(userHome, ".worktrail")
	}
	project := os.Getenv("WORKTRAIL_PROJECT_ROOT")
	if project == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Env{}, err
		}
		project = cwd
	}
	userHome, _ = filepath.Abs(userHome)
	userRoot, _ = filepath.Abs(userRoot)
	project, _ = filepath.Abs(project)
	return Env{
		Home:        userHome,
		UserRoot:    userRoot,
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}, nil
}

func (e Env) ScopeRoot(scope string) (string, error) {
	switch scope {
	case "user":
		return e.UserRoot, nil
	case "project", "":
		return e.ProjectWT, nil
	default:
		return "", errors.New("scope must be user or project")
	}
}

func SafeJoin(root string, parts ...string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(append([]string{rootAbs}, parts...)...)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", errors.New("path escapes worktrail root")
	}
	return targetAbs, nil
}

func EnsureDirs(dirs ...string) error {
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}
