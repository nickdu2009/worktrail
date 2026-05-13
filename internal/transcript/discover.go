package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DiscoveredSession struct {
	Source      string    `json:"source"`
	ID          string    `json:"id,omitempty"`
	Path        string    `json:"path"`
	ProjectRoot string    `json:"project_root"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

func DiscoverCodexSessions(home, projectRoot string) ([]DiscoveredSession, error) {
	var sessions []DiscoveredSession
	projectRoot = filepath.Clean(projectRoot)
	for _, root := range []string{
		filepath.Join(home, ".codex", "sessions"),
		filepath.Join(home, ".codex", "archived_sessions"),
	} {
		if err := walkCodexSessions(root, projectRoot, &sessions); err != nil {
			return nil, err
		}
	}
	return sessions, nil
}

func walkCodexSessions(root, projectRoot string, sessions *[]DiscoveredSession) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		session, ok, err := readCodexSessionMeta(path)
		if err != nil || !ok {
			return err
		}
		if filepath.Clean(session.ProjectRoot) != projectRoot {
			return nil
		}
		*sessions = append(*sessions, session)
		return nil
	})
}

func readCodexSessionMeta(path string) (DiscoveredSession, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return DiscoveredSession{}, false, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		if raw["type"] != "session_meta" {
			continue
		}
		payload, ok := raw["payload"].(map[string]any)
		if !ok {
			continue
		}
		cwd, _ := payload["cwd"].(string)
		if cwd == "" {
			continue
		}
		id, _ := payload["id"].(string)
		var updatedAt time.Time
		if ts, _ := raw["timestamp"].(string); ts != "" {
			updatedAt, _ = time.Parse(time.RFC3339, ts)
		}
		return DiscoveredSession{
			Source:      "codex",
			ID:          id,
			Path:        path,
			ProjectRoot: cwd,
			UpdatedAt:   updatedAt,
		}, true, nil
	}
	if err := scanner.Err(); err != nil {
		return DiscoveredSession{}, false, err
	}
	return DiscoveredSession{}, false, nil
}
