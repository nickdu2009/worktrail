package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/nickdu2009/worktrail/internal/paths"
	wtpreview "github.com/nickdu2009/worktrail/internal/preview"
)

func runPreview(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	_ = ctx
	if wantsHelp(args) {
		printPreviewHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlagsWithBooleans(args, map[string]bool{
		"no-open":     true,
		"clear-cache": true,
	})
	if err := validatePreviewFlags(flags); err != nil {
		return err
	}
	if len(positional) > 0 {
		return errors.New("worktrail preview no longer accepts a target; use `worktrail preview` or `worktrail preview --scope user`")
	}
	scope := flagValue(flags, "scope", "project")
	if flagValue(flags, "clear-cache", "") == "true" {
		dir, err := wtpreview.ClearCache(env, scope)
		if err != nil {
			return err
		}
		fmt.Fprintf(ioctx.Out, "scope\t%s\n", scope)
		fmt.Fprintf(ioctx.Out, "cleared\t%s\n", dir)
		return nil
	}

	src, err := wtpreview.Resolve(wtpreview.ResolveRequest{
		Env:   env,
		Scope: scope,
	})
	if err != nil {
		return err
	}
	cacheDir, err := wtpreview.CacheDir(env, scope)
	if err != nil {
		return err
	}
	rendered, err := wtpreview.Render(src, cacheDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(ioctx.Out, "scope\t%s\n", scope)
	fmt.Fprintf(ioctx.Out, "index\t%s\n", rendered.IndexPath)
	if flagValue(flags, "no-open", "") != "true" {
		if err := wtpreview.Open(rendered.IndexPath); err != nil {
			fmt.Fprintf(ioctx.Err, "open failed: %v\n", err)
		} else {
			fmt.Fprintf(ioctx.Out, "opened\t%s\n", rendered.IndexPath)
		}
	}
	return nil
}

func validatePreviewFlags(flags map[string]string) error {
	allowed := map[string]bool{
		"scope":       true,
		"no-open":     true,
		"clear-cache": true,
	}
	for key := range flags {
		if allowed[key] {
			continue
		}
		switch key {
		case "open", "candidate", "render-only", "format", "out":
			return fmt.Errorf("worktrail preview no longer supports --%s", key)
		default:
			return fmt.Errorf("worktrail preview: unknown flag --%s", key)
		}
	}
	return nil
}

func printPreviewHelp(out interface{ Write([]byte) (int, error) }) {
	fmt.Fprintln(out, "usage: worktrail preview [--scope project|user] [--no-open]")
	fmt.Fprintln(out, "       worktrail preview --clear-cache [--scope project|user]")
}
