package index

import (
	"io/fs"
	"path/filepath"
	"strings"
)

func scan(root, scope string) ([]Entry, error) {
	var entries []Entry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "index", "logs", "raw", "exports":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if shouldSkip(rel) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".json" {
			return nil
		}
		entry, ok, err := buildEntry(root, path, rel, scope)
		if err != nil {
			return err
		}
		if ok {
			entries = append(entries, entry)
		}
		return nil
	})
	return entries, err
}

func shouldSkip(rel string) bool {
	base := filepath.Base(rel)
	return base == "config.json" || base == ".DS_Store" || rel == "state/active/latest.md"
}
