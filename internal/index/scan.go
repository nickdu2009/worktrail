package index

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
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
	if err != nil {
		return nil, err
	}
	if err := resolveEntryIDCollisions(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func resolveEntryIDCollisions(entries []Entry) error {
	byID := make(map[string][]int, len(entries))
	for i := range entries {
		byID[entries[i].ID] = append(byID[entries[i].ID], i)
	}

	var duplicateIDs []string
	for id, indexes := range byID {
		if len(indexes) > 1 {
			duplicateIDs = append(duplicateIDs, id)
		}
	}
	sort.Strings(duplicateIDs)

	for _, id := range duplicateIDs {
		indexes := byID[id]
		var explicitPaths []string
		for _, index := range indexes {
			if !entries[index].generatedID {
				explicitPaths = append(explicitPaths, entries[index].Path)
			}
		}
		if len(explicitPaths) > 1 {
			return fmt.Errorf("duplicate explicit Worktrail id %q in %s", id, strings.Join(explicitPaths, ", "))
		}
		for _, index := range indexes {
			if !entries[index].generatedID {
				continue
			}
			sum := sha256.Sum256([]byte(entries[index].Path))
			entries[index].ID = fmt.Sprintf("%s-%x", id, sum[:6])
		}
	}

	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		if previousPath, ok := seen[entry.ID]; ok {
			return fmt.Errorf("duplicate Worktrail index id %q in %s and %s", entry.ID, previousPath, entry.Path)
		}
		seen[entry.ID] = entry.Path
	}
	return nil
}

func shouldSkip(rel string) bool {
	base := filepath.Base(rel)
	return base == "config.json" || base == ".DS_Store" || rel == "state/active/latest.md"
}
