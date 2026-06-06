package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	wlog "github.com/nickdu2009/worktrail/internal/log"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/util"
)

const (
	DirSessions    = "sessions"
	DirCheckpoints = "checkpoints"
	DirRecovery    = "recovery"
)

type Record struct {
	Meta        map[string]any
	Body        string
	Path        string
	RuntimeType string
	UpdatedAt   time.Time
}

type WriteOptions struct {
	Scope          string
	Title          string
	Body           string
	RuntimeType    string
	ResumePriority string
	SessionID      string
	TaskID         string
	SourceTool     string
	Event          string
	Tags           []string
	Actor          string
	FileSuffix     string
}

type Recorder struct {
	Root string
}

func NewRecorder(root string) Recorder {
	return Recorder{Root: strings.TrimSpace(root)}
}

func EnsureDirs(root string) error {
	return paths.EnsureDirs(
		filepath.Join(root, "runtime", DirSessions),
		filepath.Join(root, "runtime", DirCheckpoints),
		filepath.Join(root, "runtime", DirRecovery),
		filepath.Join(root, "raw", "cursor"),
		filepath.Join(root, "logs"),
	)
}

func (r Recorder) WriteSession(opts WriteOptions) (Record, error) {
	opts.RuntimeType = model.RuntimeTypeSessionState
	if opts.ResumePriority == "" {
		opts.ResumePriority = model.ResumePriorityHookRuntimeState
	}
	return r.write(filepath.Join("runtime", DirSessions), opts)
}

func (r Recorder) WriteCheckpoint(opts WriteOptions) (Record, error) {
	opts.RuntimeType = model.RuntimeTypeCheckpoint
	if opts.ResumePriority == "" {
		opts.ResumePriority = model.ResumePriorityRuntimeCheckpoint
	}
	return r.write(filepath.Join("runtime", DirCheckpoints), opts)
}

func (r Recorder) WriteTakeoverNote(opts WriteOptions) (Record, error) {
	opts.RuntimeType = model.RuntimeTypeTakeoverNote
	if opts.ResumePriority == "" {
		opts.ResumePriority = model.ResumePriorityRuntimeCheckpoint
	}
	return r.write(filepath.Join("runtime", DirCheckpoints), opts)
}

func (r Recorder) write(relDir string, opts WriteOptions) (Record, error) {
	if strings.TrimSpace(r.Root) == "" {
		return Record{}, errors.New("runtime recorder root is required")
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "Worktrail runtime"
	}
	now := time.Now().UTC()
	idPrefix := "rt"
	switch opts.RuntimeType {
	case model.RuntimeTypeCheckpoint:
		idPrefix = "chk"
	case model.RuntimeTypeTakeoverNote:
		idPrefix = "tko"
	case model.RuntimeTypeSessionState:
		idPrefix = "ses"
	}
	id := fmt.Sprintf("%s_%s_%s", idPrefix, now.Format("20060102_150405.000000000"), util.Slug(title))
	fileName := runtimeFileName(id, opts, now)
	meta := map[string]any{
		"schema":           model.SchemaRuntimeV2,
		"id":               id,
		"object_kind":      model.ObjectKindRuntime,
		"scope":            withDefault(opts.Scope, "project"),
		"runtime_type":     opts.RuntimeType,
		"title":            title,
		"durability":       model.DurabilityEphemeral,
		"lifecycle_status": model.LifecycleActive,
		"resume_priority":  opts.ResumePriority,
		"session_id":       strings.TrimSpace(opts.SessionID),
		"task_id":          strings.TrimSpace(opts.TaskID),
		"source_tool":      strings.TrimSpace(opts.SourceTool),
		"created_at":       now,
		"updated_at":       now,
		"tags":             cleanList(append([]string{opts.Event}, opts.Tags...)),
	}
	data, err := store.RenderMarkdown(meta, opts.Body)
	if err != nil {
		return Record{}, err
	}
	path, err := paths.SafeJoin(r.Root, relDir, fileName)
	if err != nil {
		return Record{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Record{}, err
	}
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return Record{}, err
	}
	actor := withDefault(opts.Actor, "runtime")
	if err := wlog.Append(r.Root, "runtime.write", id, actor, map[string]any{
		"path":            relToRoot(r.Root, path),
		"runtime_type":    opts.RuntimeType,
		"resume_priority": opts.ResumePriority,
	}); err != nil {
		return Record{}, err
	}
	return Record{
		Meta:        meta,
		Body:        opts.Body,
		Path:        path,
		RuntimeType: opts.RuntimeType,
		UpdatedAt:   now,
	}, nil
}

func runtimeFileName(id string, opts WriteOptions, now time.Time) string {
	base := strings.TrimSpace(opts.FileSuffix)
	if base == "" {
		base = now.Format("20060102-150405.000000000")
	}
	parts := []string{base}
	if sessionKey := runtimeSessionKey(opts.SessionID); sessionKey != "" {
		parts = append(parts, sessionKey)
	}
	parts = append(parts, shortHash(id))
	return strings.Join(parts, "-") + ".md"
}

func runtimeSessionKey(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	return "session-" + shortHash(sessionID)
}

func (r Recorder) WriteRecoveryDashboard(body string) (string, error) {
	now := time.Now().UTC()
	meta := map[string]any{
		"schema":           model.SchemaRuntimeV2,
		"id":               "recovery_dashboard_" + now.Format("20060102_150405"),
		"object_kind":      model.ObjectKindRuntime,
		"scope":            "project",
		"runtime_type":     "recovery_dashboard",
		"title":            "Recovery Dashboard",
		"durability":       model.DurabilityEphemeral,
		"lifecycle_status": model.LifecycleCurrent,
		"created_at":       now,
		"updated_at":       now,
		"tags":             []string{"recovery", "dashboard"},
	}
	data, err := store.RenderMarkdown(meta, body)
	if err != nil {
		return "", err
	}
	path := filepath.Join(r.Root, "runtime", DirRecovery, "current-state.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func List(env paths.Env, scope, dir string) ([]Record, error) {
	root, _, err := scopeRoot(env, scope)
	if err != nil {
		return nil, err
	}
	switch dir {
	case DirSessions, DirCheckpoints, DirRecovery:
	default:
		return nil, fmt.Errorf("unknown runtime directory %q", dir)
	}
	target, err := paths.SafeJoin(root, "runtime", dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path, err := paths.SafeJoin(target, entry.Name())
		if err != nil {
			return nil, err
		}
		rec, err := Read(path)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func Latest(env paths.Env, scope, dir string) (Record, error) {
	items, err := List(env, scope, dir)
	if err != nil {
		return Record{}, err
	}
	if len(items) == 0 {
		return Record{}, os.ErrNotExist
	}
	return items[0], nil
}

func Read(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	doc, err := store.ParseMarkdown(data)
	if err != nil {
		return Record{}, err
	}
	runtimeType := stringField(doc.Meta, "runtime_type")
	updatedAt := timeField(doc.Meta, "updated_at")
	return Record{
		Meta:        doc.Meta,
		Body:        doc.Body,
		Path:        path,
		RuntimeType: runtimeType,
		UpdatedAt:   updatedAt,
	}, nil
}

func scopeRoot(env paths.Env, scope string) (string, string, error) {
	resolved := strings.TrimSpace(scope)
	if resolved == "" {
		resolved = "project"
	}
	root, err := env.ScopeRoot(resolved)
	if err != nil {
		return "", "", err
	}
	return root, resolved, nil
}

func relToRoot(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func withDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func cleanList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func StringField(meta map[string]any, key string) string {
	return stringField(meta, key)
}

func stringField(meta map[string]any, key string) string {
	raw, ok := meta[key]
	if !ok {
		return ""
	}
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func timeField(meta map[string]any, key string) time.Time {
	raw, ok := meta[key]
	if !ok {
		return time.Time{}
	}
	switch v := raw.(type) {
	case time.Time:
		return v
	case string:
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	if b, err := json.Marshal(raw); err == nil {
		var t time.Time
		if json.Unmarshal(b, &t) == nil {
			return t
		}
	}
	return time.Time{}
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}
