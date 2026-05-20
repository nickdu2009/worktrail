package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/paths"
	wtpreview "github.com/nickdu2009/worktrail/internal/preview"
)

type previewJSON struct {
	Source    wtpreview.Source `json:"source"`
	OutputDir string           `json:"output_dir"`
	IndexPath string           `json:"index_path"`
	URL       string           `json:"url,omitempty"`
	Temporary bool             `json:"temporary"`
}

func runPreview(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		printPreviewHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlagsWithBooleans(args, map[string]bool{
		"open":        true,
		"render-only": true,
	})
	scope := flagValue(flags, "scope", "project")
	target := firstArg(positional, "")
	req := wtpreview.ResolveRequest{
		Env:         env,
		Scope:       scope,
		Target:      target,
		CandidateID: flagValue(flags, "candidate", ""),
	}
	if req.Target == "" && req.CandidateID == "" {
		return errors.New("usage: worktrail preview <target> [--scope project|user]")
	}
	open := flagValue(flags, "open", "") == "true"
	renderOnly := flagValue(flags, "render-only", "") == "true"
	if renderOnly && open {
		return errors.New("worktrail preview: --render-only cannot be used with --open")
	}

	src, err := wtpreview.Resolve(req)
	if err != nil {
		if errors.Is(err, candidate.ErrNotFound) {
			return fmt.Errorf("%w; next: worktrail candidates list --scope %s", err, scope)
		}
		return err
	}
	rendered, err := wtpreview.Render(src, flagValue(flags, "out", ""))
	if err != nil {
		return err
	}

	if renderOnly {
		return printPreviewRenderOnly(ioctx, rendered, flagValue(flags, "format", "text"))
	}

	defer func() {
		if rendered.Temporary {
			_ = os.RemoveAll(rendered.OutputDir)
		}
	}()
	served, err := wtpreview.Serve(ctx, rendered.OutputDir)
	if err != nil {
		return err
	}
	defer func() {
		_ = served.Stop()
	}()
	if open {
		if err := wtpreview.Open(served.URL); err != nil {
			fmt.Fprintf(ioctx.Err, "open failed: %v\n", err)
		}
	}
	fmt.Fprintf(ioctx.Out, "source\t%s\n", rendered.Source.Path)
	fmt.Fprintf(ioctx.Out, "url\t%s\n", served.URL)
	fmt.Fprintln(ioctx.Out, "stop\tpress Ctrl-C to stop preview")
	<-ctx.Done()
	return nil
}

func printPreviewRenderOnly(ioctx IO, rendered wtpreview.RenderResult, format string) error {
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(previewJSON{
			Source:    rendered.Source,
			OutputDir: rendered.OutputDir,
			IndexPath: rendered.IndexPath,
			Temporary: rendered.Temporary,
		})
	}
	fmt.Fprintf(ioctx.Out, "source\t%s\n", rendered.Source.Path)
	fmt.Fprintf(ioctx.Out, "index\t%s\n", rendered.IndexPath)
	return nil
}

func printPreviewHelp(out interface{ Write([]byte) (int, error) }) {
	fmt.Fprintln(out, "usage: worktrail preview <target> [--scope project|user] [--open]")
	fmt.Fprintln(out, "       worktrail preview --candidate <id> [--scope project|user] [--open]")
	fmt.Fprintln(out, "       worktrail preview <target> --render-only [--out dir] [--format json]")
}
