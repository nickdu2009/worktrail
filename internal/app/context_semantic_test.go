package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/contextpack"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

func TestRunContextPackWithoutSemanticPreservesMarkdownOutput(t *testing.T) {
	env := semanticSearchTestEnv(t)
	args := []string{"context task"}
	var baselineOut, baselineErr, gotOut, gotErr bytes.Buffer

	if err := runContextPack(context.Background(), env, IO{Out: &baselineOut, Err: &baselineErr}, args); err != nil {
		t.Fatalf("run context baseline: %v", err)
	}
	selector := &contextSemanticSelectorStub{}
	if err := runContextPackWithSemantic(context.Background(), env, IO{Out: &gotOut, Err: &gotErr}, args, selector); err != nil {
		t.Fatalf("run context without semantic: %v", err)
	}
	if gotOut.String() != baselineOut.String() || gotErr.String() != baselineErr.String() {
		t.Fatalf("non-semantic output changed:\nstdout got=%q\nstdout want=%q\nstderr got=%q\nstderr want=%q", gotOut.String(), baselineOut.String(), gotErr.String(), baselineErr.String())
	}
	if len(selector.requests) != 0 {
		t.Fatalf("selector calls = %d, want 0", len(selector.requests))
	}
}

func TestRunContextPackSemanticModesInjectSelector(t *testing.T) {
	for _, arg := range []string{"--semantic", "--semantic=auto", "--semantic=required"} {
		t.Run(arg, func(t *testing.T) {
			env := semanticSearchTestEnv(t)
			selector := &contextSemanticSelectorStub{}
			var out, errw bytes.Buffer

			if err := runContextPackWithSemantic(context.Background(), env, IO{Out: &out, Err: &errw}, []string{arg, "--format=json", "semantic context task"}, selector); err != nil {
				t.Fatalf("run context %s: %v", arg, err)
			}
			if len(selector.requests) == 0 {
				t.Fatal("selector was not called")
			}
			if selector.requests[0].Task != "semantic context task" {
				t.Fatalf("selector task = %q", selector.requests[0].Task)
			}
			var pack contextpack.Pack
			if err := json.Unmarshal(out.Bytes(), &pack); err != nil {
				t.Fatalf("unmarshal context pack: %v stdout=%s", err, out.String())
			}
			if pack.Schema != "worktrail.context_pack.v2" {
				t.Fatalf("schema = %q", pack.Schema)
			}
			if errw.Len() != 0 {
				t.Fatalf("semantic success wrote stderr: %q", errw.String())
			}
		})
	}
}

func TestRunContextPackRejectsInvalidSemanticArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--semantic", "auto", "task"},
		{"--semantic", "required", "task"},
		{"--semantic=lexical", "task"},
		{"--semantic=invalid", "task"},
		{"--semantic=", "task"},
		{"--semantic=auto", "--semantic=required", "task"},
		{"--semantic", "--semantic=auto", "task"},
		{"--semantic-mode=auto", "task"},
		{"--explain", "task"},
		{"--explain=true", "task"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := runContextPackWithSemantic(context.Background(), semanticSearchTestEnv(t), IO{}, args, &contextSemanticSelectorStub{})
			if err == nil || !strings.Contains(err.Error(), "invalid semantic arguments") {
				t.Fatalf("run context %v error = %v", args, err)
			}
		})
	}
}

func TestRunContextPackAutoFallbackReturnsDeterministicPack(t *testing.T) {
	env := semanticSearchTestEnv(t)
	args := []string{"--format=json", "context task"}
	var deterministicOut, fallbackOut, fallbackErr bytes.Buffer

	if err := runContextPack(context.Background(), env, IO{Out: &deterministicOut}, args); err != nil {
		t.Fatalf("run deterministic context: %v", err)
	}
	selector := &contextSemanticSelectorStub{err: errors.New("selector unavailable")}
	if err := runContextPackWithSemantic(context.Background(), env, IO{Out: &fallbackOut, Err: &fallbackErr}, append([]string{"--semantic"}, args...), selector); err != nil {
		t.Fatalf("run semantic fallback: %v", err)
	}
	var deterministic, fallback contextpack.Pack
	if err := json.Unmarshal(deterministicOut.Bytes(), &deterministic); err != nil {
		t.Fatalf("unmarshal deterministic pack: %v", err)
	}
	if err := json.Unmarshal(fallbackOut.Bytes(), &fallback); err != nil {
		t.Fatalf("unmarshal fallback pack: %v stdout=%s", err, fallbackOut.String())
	}
	deterministic.CreatedAt = time.Time{}
	fallback.CreatedAt = time.Time{}
	if !reflect.DeepEqual(fallback, deterministic) {
		t.Fatalf("fallback pack differs from deterministic pack:\ngot=%#v\nwant=%#v", fallback, deterministic)
	}
	for _, want := range []string{"semantic context fallback (auto)", string(contracts.ReasonRuntimeUnavailable), "worktrail semantic rebuild --scope all"} {
		if !strings.Contains(fallbackErr.String(), want) {
			t.Fatalf("fallback stderr missing %q:\n%s", want, fallbackErr.String())
		}
	}
}

func TestRunContextPackAutoFallbackPreservesSemanticReason(t *testing.T) {
	var out, errw bytes.Buffer
	err := runContextPackWithSemantic(
		context.Background(),
		semanticSearchTestEnv(t),
		IO{Out: &out, Err: &errw},
		[]string{"--semantic=auto", "context task"},
		&contextSemanticSelectorStub{err: &SemanticSearchError{Code: contracts.ReasonProfileStale}},
	)
	if err != nil {
		t.Fatalf("run context fallback: %v", err)
	}
	if !strings.Contains(errw.String(), string(contracts.ReasonProfileStale)) {
		t.Fatalf("fallback lost semantic reason:\n%s", errw.String())
	}
}

func TestRunContextPackRequiredReturnsTypedAndJSONErrors(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		var out, errw bytes.Buffer
		err := runContextPackWithSemantic(context.Background(), semanticSearchTestEnv(t), IO{Out: &out, Err: &errw}, []string{"--semantic=required", "task"}, &contextSemanticSelectorStub{err: errors.New("selector unavailable")})
		var semanticErr *SemanticSearchError
		if !errors.As(err, &semanticErr) || semanticErr.Code != contracts.ReasonRuntimeUnavailable {
			t.Fatalf("required error = %v, want runtime unavailable SemanticSearchError", err)
		}
		if out.Len() != 0 || errw.Len() != 0 {
			t.Fatalf("required error wrote stdout=%q stderr=%q", out.String(), errw.String())
		}
	})

	t.Run("json required", func(t *testing.T) {
		var out bytes.Buffer
		err := runContextPackWithSemantic(context.Background(), semanticSearchTestEnv(t), IO{Out: &out}, []string{"--semantic=required", "--format=json", "task"}, &contextSemanticSelectorStub{err: errors.New("selector unavailable")})
		if err != nil {
			t.Fatalf("required JSON error = %v", err)
		}
		assertCLIErrorEnvelope(t, out.String(), string(contracts.ReasonRuntimeUnavailable))
	})

	t.Run("json usage", func(t *testing.T) {
		var out bytes.Buffer
		err := runContextPackWithSemantic(context.Background(), semanticSearchTestEnv(t), IO{Out: &out}, []string{"--format=json", "--semantic", "auto", "task"}, &contextSemanticSelectorStub{})
		if err != nil {
			t.Fatalf("usage JSON error = %v", err)
		}
		assertCLIErrorEnvelope(t, out.String(), "cli_usage_error")
	})
}

func TestPrintContextHelpListsSemanticModes(t *testing.T) {
	var out bytes.Buffer
	printContextHelp(&out)
	if !strings.Contains(out.String(), "--semantic|--semantic=auto|--semantic=required") {
		t.Fatalf("context help missing semantic modes: %s", out.String())
	}
}

type contextSemanticSelectorStub struct {
	requests []contextpack.SelectionRequest
	err      error
}

func (s *contextSemanticSelectorStub) Select(request contextpack.SelectionRequest) ([]contextpack.Item, error) {
	s.requests = append(s.requests, request)
	if s.err != nil {
		return nil, s.err
	}
	if len(request.Candidates) == 0 {
		return nil, nil
	}
	return request.Candidates[:1], nil
}
