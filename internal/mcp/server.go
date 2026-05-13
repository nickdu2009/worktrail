package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/redact"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/util"
)

var canonicalTools = []string{
	"worktrail.search",
	"worktrail.context_pack",
	"worktrail.state.active",
	"worktrail.state.read",
	"worktrail.state.create",
	"worktrail.state.update",
	"worktrail.candidate.create",
	"worktrail.candidate.list",
	"worktrail.candidate.show",
	"worktrail.candidate.diff",
	"worktrail.handoff.create",
	"worktrail.redact.scan",
}

var resourceURIs = []string{
	"user://brief",
	"user://profile/preferences",
	"user://workflows",
	"project://overview",
	"project://current-state",
	"project://decisions",
	"project://handoffs",
	"project://active-state",
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Server struct {
	Env paths.Env
}

func Serve(ctx context.Context, env paths.Env, in io.Reader, out io.Writer) error {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	server := Server{Env: env}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			if err := enc.Encode(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}}); err != nil {
				return err
			}
			continue
		}
		if req.ID == nil {
			continue
		}
		result, rpcErr := server.handle(ctx, req)
		resp := response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
		if rpcErr != nil {
			resp.Result = nil
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s Server) Handle(ctx context.Context, data []byte) ([]byte, error) {
	var req request
	if err := json.Unmarshal(data, &req); err != nil {
		resp := response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}}
		return json.Marshal(resp)
	}
	result, rpcErr := s.handle(ctx, req)
	resp := response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
	if rpcErr != nil {
		resp.Result = nil
	}
	return json.Marshal(resp)
}

func (s Server) handle(ctx context.Context, req request) (any, *rpcError) {
	select {
	case <-ctx.Done():
		return nil, &rpcError{Code: -32000, Message: ctx.Err().Error()}
	default:
	}
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]any{"name": "worktrail", "version": "0.1.0"},
			"capabilities":    map[string]any{"tools": map[string]any{}, "resources": map[string]any{}},
		}, nil
	case "tools/list":
		return map[string]any{"tools": toolsList()}, nil
	case "tools/call":
		result, err := s.callTool(req.Params)
		if err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		return result, nil
	case "resources/list":
		return map[string]any{"resources": resourcesList()}, nil
	case "resources/read":
		result, err := s.readResource(req.Params)
		if err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		return result, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

func toolsList() []map[string]any {
	tools := make([]map[string]any, 0, len(canonicalTools))
	for _, name := range canonicalTools {
		tools = append(tools, map[string]any{
			"name":        name,
			"description": toolDescription(name),
			"inputSchema": map[string]any{"type": "object", "additionalProperties": true},
		})
	}
	return tools
}

func resourcesList() []map[string]any {
	resources := make([]map[string]any, 0, len(resourceURIs))
	for _, uri := range resourceURIs {
		resources = append(resources, map[string]any{
			"uri":      uri,
			"name":     strings.TrimPrefix(strings.ReplaceAll(uri, "://", " "), "user "),
			"mimeType": "text/markdown",
		})
	}
	return resources
}

func (s Server) callTool(params json.RawMessage) (any, error) {
	var req struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	if !isCanonicalTool(req.Name) {
		return nil, fmt.Errorf("unsupported Worktrail tool %q", req.Name)
	}
	var text string
	var err error
	switch req.Name {
	case "worktrail.search":
		text, err = s.search(req.Arguments)
	case "worktrail.context_pack":
		text, err = s.contextPack(req.Arguments)
	case "worktrail.state.active":
		text, err = s.activeState()
	case "worktrail.state.read":
		text, err = s.readState(req.Arguments)
	case "worktrail.state.create":
		text, err = s.writeState(req.Arguments, false)
	case "worktrail.state.update":
		text, err = s.writeState(req.Arguments, true)
	case "worktrail.candidate.create":
		text, err = s.createCandidate(req.Arguments, "knowledge")
	case "worktrail.candidate.list":
		text, err = s.listCandidates()
	case "worktrail.candidate.show":
		text, err = s.showCandidate(req.Arguments)
	case "worktrail.candidate.diff":
		text, err = s.diffCandidate(req.Arguments)
	case "worktrail.handoff.create":
		text, err = s.createCandidate(req.Arguments, "handoff")
	case "worktrail.redact.scan":
		text, err = s.scanRedaction(req.Arguments)
	default:
		err = errors.New("tool not implemented")
	}
	if err != nil {
		return nil, err
	}
	return textContent(text), nil
}

func (s Server) showCandidate(args map[string]any) (string, error) {
	id, _ := args["id"].(string)
	scope := fallbackString(args["scope"], "project")
	rec, err := (candidate.Manager{Env: s.Env, Actor: "mcp:candidate-show"}).Show(scope, id)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	return string(data), err
}

func (s Server) diffCandidate(args map[string]any) (string, error) {
	id, _ := args["id"].(string)
	scope := fallbackString(args["scope"], "project")
	return (candidate.Manager{Env: s.Env, Actor: "mcp:candidate-diff"}).Diff(scope, id)
}

func (s Server) scanRedaction(args map[string]any) (string, error) {
	text, _ := args["text"].(string)
	result := redact.Scan(text)
	data, err := json.MarshalIndent(result, "", "  ")
	return string(data), err
}

func (s Server) readResource(params json.RawMessage) (any, error) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	text, err := s.resourceText(req.URI)
	if err != nil {
		return nil, err
	}
	return map[string]any{"contents": []map[string]any{{"uri": req.URI, "mimeType": "text/markdown", "text": text}}}, nil
}

func (s Server) search(args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	items := []string{}
	for _, path := range []string{
		filepath.Join(s.Env.UserRoot, "index.md"),
		filepath.Join(s.Env.ProjectWT, "index.md"),
		filepath.Join(s.Env.ProjectWT, "current-state.md"),
	} {
		data, err := os.ReadFile(path)
		if err == nil && (query == "" || strings.Contains(strings.ToLower(string(data)), strings.ToLower(query))) {
			items = append(items, path)
		}
	}
	data, _ := json.MarshalIndent(map[string]any{"query": query, "results": items}, "", "  ")
	return string(data), nil
}

func (s Server) contextPack(args map[string]any) (string, error) {
	task, _ := args["task"].(string)
	if task == "" {
		task, _ = args["query"].(string)
	}
	active, _ := s.activeState()
	return "# Context Pack\n\n## Task\n" + fallback(task, "Unspecified task") + "\n\n## Active State\n" + active + "\n\n## Task Instruction\nUse Worktrail context and leave candidates pending for review.\n", nil
}

func (s Server) activeState() (string, error) {
	data, err := os.ReadFile(filepath.Join(s.Env.ProjectWT, "state", "active", "latest.md"))
	if errors.Is(err, os.ErrNotExist) {
		return "No active state.", nil
	}
	return string(data), err
}

func (s Server) readState(args map[string]any) (string, error) {
	id, _ := args["id"].(string)
	if id == "" || id == "latest" {
		return s.activeState()
	}
	path, err := paths.SafeJoin(filepath.Join(s.Env.ProjectWT, "state", "active"), id+".md")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	return string(data), err
}

func (s Server) writeState(args map[string]any, update bool) (string, error) {
	now := time.Now()
	title, _ := args["title"].(string)
	if title == "" {
		title, _ = args["task"].(string)
	}
	title = fallback(title, "MCP State")
	id := "st_" + now.Format("20060102_150405") + "_" + util.Slug(title)
	if update {
		id = "st_latest_" + util.Slug(title)
	}
	state := model.State{
		Schema:     model.SchemaState,
		ID:         id,
		Scope:      "project",
		Type:       fallbackString(args["type"], "implementation"),
		Title:      title,
		Status:     "active",
		SourceTool: "mcp",
		CreatedAt:  now,
		UpdatedAt:  now,
		Tags:       []string{"mcp"},
	}
	body := "# State Capsule: " + title + "\n\n" + section("Current Goal", title) + section("Evidence", fallbackString(args["evidence"], "Created through MCP.")) + section("Next Step", fallbackString(args["next_step"], "Continue from active state."))
	data, err := store.RenderMarkdown(state, body)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.Env.ProjectWT, "state", "active", "latest.md")
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (s Server) createCandidate(args map[string]any, candidateType string) (string, error) {
	now := time.Now()
	title := fallbackString(args["title"], "MCP Candidate")
	summary := fallbackString(args["summary"], "Draft candidate created through MCP.")
	target := fallbackString(args["target_path"], ".worktrail/candidates/project/"+util.Slug(title)+".md")
	candidate := model.Candidate{
		Schema:          model.SchemaCandidate,
		ID:              "cand_" + now.Format("20060102_150405") + "_" + util.Slug(title),
		Scope:           "project",
		CandidateType:   candidateType,
		TargetPath:      target,
		Title:           title,
		Summary:         summary,
		Operation:       fallbackString(args["operation"], "create"),
		Status:          "pending",
		RedactionStatus: "clean",
		CreatedAt:       now,
		UpdatedAt:       now,
		Tags:            []string{"mcp"},
	}
	body := "# Candidate: " + title + "\n\n" + section("Proposed Content", fallbackString(args["content"], summary)) + section("Review Required", "This MCP draft remains pending and must be reviewed before promotion.")
	data, err := store.RenderMarkdown(candidate, body)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.Env.ProjectWT, "candidates", "project", candidate.ID+".md")
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (s Server) listCandidates() (string, error) {
	dir := filepath.Join(s.Env.ProjectWT, "candidates", "project")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "[]", nil
	}
	if err != nil {
		return "", err
	}
	items := []map[string]any{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		doc, err := store.ParseMarkdown(data)
		if err != nil {
			continue
		}
		doc.Meta["path"] = path
		items = append(items, doc.Meta)
	}
	data, err := json.MarshalIndent(items, "", "  ")
	return string(data), err
}

func (s Server) resourceText(uri string) (string, error) {
	switch uri {
	case "user://brief":
		return readOptional(filepath.Join(s.Env.UserRoot, "index.md"), "# Worktrail User Brief\n")
	case "user://profile/preferences":
		return readOptional(filepath.Join(s.Env.UserRoot, "profile", "preferences.md"), "# Preferences\n")
	case "user://workflows":
		return concatDir(filepath.Join(s.Env.UserRoot, "workflows"))
	case "project://overview":
		return readOptional(filepath.Join(s.Env.ProjectWT, "project.md"), "# Project\n")
	case "project://current-state":
		return readOptional(filepath.Join(s.Env.ProjectWT, "current-state.md"), "# Current State\n")
	case "project://decisions":
		return concatDir(filepath.Join(s.Env.ProjectWT, "decisions"))
	case "project://handoffs":
		return concatDir(filepath.Join(s.Env.ProjectWT, "handoffs"))
	case "project://active-state":
		return s.activeState()
	default:
		return "", fmt.Errorf("unknown resource URI %q", uri)
	}
}

func readOptional(path, fallback string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fallback, nil
	}
	return string(data), err
}

func concatDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "No entries.", nil
	}
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		b.WriteString("\n\n# ")
		b.WriteString(entry.Name())
		b.WriteString("\n\n")
		b.Write(data)
	}
	if b.Len() == 0 {
		return "No entries.", nil
	}
	return b.String(), nil
}

func textContent(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func isCanonicalTool(name string) bool {
	for _, tool := range canonicalTools {
		if name == tool {
			return true
		}
	}
	return false
}

func toolDescription(name string) string {
	switch name {
	case "worktrail.search":
		return "Search local Worktrail source files."
	case "worktrail.context_pack":
		return "Build a Context Pack for a task."
	case "worktrail.state.active":
		return "Read the active State Capsule."
	case "worktrail.state.read":
		return "Read a State Capsule."
	case "worktrail.state.create":
		return "Create a draft active State Capsule."
	case "worktrail.state.update":
		return "Update the active State Capsule."
	case "worktrail.candidate.create":
		return "Create a pending candidate draft."
	case "worktrail.candidate.list":
		return "List pending candidate drafts."
	case "worktrail.candidate.show":
		return "Show a pending candidate draft."
	case "worktrail.candidate.diff":
		return "Preview a candidate diff."
	case "worktrail.handoff.create":
		return "Create a pending handoff candidate."
	case "worktrail.redact.scan":
		return "Scan provided text for redaction findings."
	default:
		return "Worktrail tool."
	}
}

func fallback(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return strings.TrimSpace(value)
}

func fallbackString(value any, def string) string {
	if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return def
}

func section(name, body string) string {
	return "## " + name + "\n" + strings.TrimSpace(body) + "\n\n"
}
