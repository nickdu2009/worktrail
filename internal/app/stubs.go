package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/nickdu2009/worktrail/internal/paths"
)

func runState(context.Context, paths.Env, IO, []string) error {
	return errors.New("state commands not implemented yet")
}
func runCandidates(context.Context, paths.Env, IO, []string) error {
	return errors.New("candidate commands not implemented yet")
}
func runReview(context.Context, paths.Env, IO, []string) error {
	return errors.New("review not implemented yet")
}
func runCandidateAction(context.Context, paths.Env, IO, string, []string) error {
	return errors.New("candidate action not implemented yet")
}
func runMerge(context.Context, paths.Env, IO, []string) error {
	return errors.New("merge not implemented yet")
}
func runRedact(context.Context, paths.Env, IO, []string) error {
	return errors.New("redact not implemented yet")
}
func runIndex(context.Context, paths.Env, IO, []string) error {
	return errors.New("index not implemented yet")
}
func runSearch(context.Context, paths.Env, IO, []string) error {
	return errors.New("search not implemented yet")
}
func runContextPack(context.Context, paths.Env, IO, []string) error {
	return errors.New("context not implemented yet")
}
func runSync(context.Context, paths.Env, IO, []string) error {
	return errors.New("sync not implemented yet")
}
func runExtract(context.Context, paths.Env, IO, []string) error {
	return errors.New("extract not implemented yet")
}
func runInstall(context.Context, paths.Env, IO, []string) error {
	return errors.New("install not implemented yet")
}
func runUninstall(context.Context, paths.Env, IO, []string) error {
	return errors.New("uninstall not implemented yet")
}
func runDoctor(_ context.Context, env paths.Env, ioctx IO, _ []string) error {
	fmt.Fprintf(ioctx.Out, "user: %s\nproject: %s\n", env.UserRoot, env.ProjectWT)
	return nil
}
func runHook(context.Context, paths.Env, IO, []string) error {
	return errors.New("hook not implemented yet")
}
func runMCP(context.Context, paths.Env, IO, []string) error {
	return errors.New("mcp not implemented yet")
}
func runHandoff(context.Context, paths.Env, IO, []string) error {
	return errors.New("handoff not implemented yet")
}
func runADR(context.Context, paths.Env, IO, []string) error {
	return errors.New("adr not implemented yet")
}
