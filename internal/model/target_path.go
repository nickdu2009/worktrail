package model

import (
	"path/filepath"
	"strings"
)

func NormalizeTargetPath(targetPath string) string {
	targetPath = filepath.ToSlash(strings.TrimSpace(targetPath))
	if strings.HasPrefix(targetPath, ".worktrail/") {
		return strings.TrimPrefix(targetPath, ".worktrail/")
	}
	return targetPath
}
