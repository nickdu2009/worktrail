package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/util"
)

func runHandoff(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	flags, positional := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	title := flagValue(flags, "title", "Handoff")
	summary := joinArgs(positional)
	if summary == "" {
		summary = "Draft handoff generated from the current Worktrail state."
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	rec, err := (candidate.Manager{Env: env, Actor: "cli:handoff"}).Create(candidate.CreateRequest{
		Scope:         scope,
		CandidateType: "handoff",
		TargetPath:    filepath.ToSlash(filepath.Join("handoffs", stamp+"-"+util.Slug(title)+".md")),
		Title:         title,
		Summary:       summary,
		Operation:     "create",
		Body:          "# Handoff: " + title + "\n\n" + summary + "\n",
	})
	if err != nil {
		return err
	}
	return printCandidate(ioctx, rec, flagValue(flags, "format", "text"))
}

func runADR(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 || args[0] != "create" {
		return errors.New("usage: worktrail adr create <title>")
	}
	flags, positional := splitFlags(args[1:])
	scope := flagValue(flags, "scope", "project")
	title := joinArgs(positional)
	if title == "" {
		title = flagValue(flags, "title", "Architecture Decision")
	}
	stamp := time.Now().UTC().Format("20060102")
	body := fmt.Sprintf("# ADR: %s\n\n## Status\n\nProposed\n\n## Context\n\n## Decision\n\n## Consequences\n", title)
	rec, err := (candidate.Manager{Env: env, Actor: "cli:adr"}).Create(candidate.CreateRequest{
		Scope:         scope,
		CandidateType: "adr",
		TargetPath:    filepath.ToSlash(filepath.Join("decisions", "ADR-"+stamp+"-"+util.Slug(title)+".md")),
		Title:         title,
		Summary:       "Draft ADR candidate.",
		Operation:     "create",
		Body:          body,
	})
	if err != nil {
		return err
	}
	return printCandidate(ioctx, rec, flagValue(flags, "format", "text"))
}
