package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
)

func mcpEnv(t *testing.T) paths.Env {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	return paths.Env{
		Home:        home,
		UserRoot:    filepath.Join(home, ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
}

func TestToolsListIsCanonicalAndExcludesDangerousTools(t *testing.T) {
	env := mcpEnv(t)
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	raw, err := (Server{Env: env}).Handle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)
	got := map[string]bool{}
	for _, item := range tools {
		name := item.(map[string]any)["name"].(string)
		got[name] = true
		for _, forbidden := range []string{"promote", "merge", "discard", "delete", "replace"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("dangerous tool exposed: %s", name)
			}
		}
	}
	for _, want := range canonicalTools {
		if !got[want] {
			t.Fatalf("missing canonical tool %s in %#v", want, got)
		}
	}
}

func TestJSONRPCServeCreatesAndListsPendingCandidate(t *testing.T) {
	env := mcpEnv(t)
	var in bytes.Buffer
	in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"worktrail.candidate.create","arguments":{"title":"Review Hook Draft","summary":"Pending only"}}}` + "\n")
	in.WriteString(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"worktrail.candidate.list","arguments":{}}}` + "\n")
	var out bytes.Buffer
	if err := Serve(context.Background(), env, &in, &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if !strings.Contains(out.String(), "Review Hook Draft") {
		t.Fatalf("expected candidate in output:\n%s", out.String())
	}
	if strings.Contains(out.String(), "promote") {
		t.Fatalf("MCP response should not expose promote:\n%s", out.String())
	}
	files, err := filepath.Glob(filepath.Join(env.ProjectWT, "candidates", "project", "cand_*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one candidate file, got %d", len(files))
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "pending"`) {
		t.Fatalf("candidate should stay pending:\n%s", data)
	}
}

func TestResourcesReadActiveState(t *testing.T) {
	env := mcpEnv(t)
	if err := os.MkdirAll(filepath.Join(env.ProjectWT, "state", "active"), 0o755); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(env.ProjectWT, "state", "active", "latest.md")
	if err := os.WriteFile(active, []byte("# Active\nKeep going.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := []byte(`{"jsonrpc":"2.0","id":"r1","method":"resources/read","params":{"uri":"project://active-state"}}`)
	raw, err := (Server{Env: env}).Handle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Keep going") {
		t.Fatalf("expected active state in resource response: %s", raw)
	}
}

func TestUnsupportedDangerousToolErrors(t *testing.T) {
	env := mcpEnv(t)
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"worktrail.candidate.promote","arguments":{}}}`)
	raw, err := (Server{Env: env}).Handle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "unsupported Worktrail tool") {
		t.Fatalf("expected unsupported tool error: %s", raw)
	}
}
