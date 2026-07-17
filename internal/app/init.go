package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

type SemanticInstaller interface {
	Install(context.Context, paths.Env) (SemanticInstallInfo, error)
}

// SemanticInstallInfo exposes the selected trusted runtime without changing
// the core initialization contract.
type SemanticInstallInfo struct {
	SupportLevel string
	Chip         string
	Warning      string
}

type semanticInstallerFunc func(context.Context, paths.Env) (SemanticInstallInfo, error)

func (f semanticInstallerFunc) Install(ctx context.Context, env paths.Env) (SemanticInstallInfo, error) {
	return f(ctx, env)
}

var defaultSemanticInstaller SemanticInstaller = newProductionSemanticInstaller()

func runInit(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	return runInitWithInstaller(ctx, env, ioctx, args, defaultSemanticInstaller)
}

func runInitWithInstaller(ctx context.Context, env paths.Env, ioctx IO, args []string, installer SemanticInstaller) error {
	semantic, err := parseInitSemanticFlag(args)
	if err != nil {
		return err
	}
	if err := store.InitUser(env); err != nil {
		return err
	}
	if err := store.InitProject(env); err != nil {
		return err
	}
	fmt.Fprintln(ioctx.Out, "initialized user and project worktrail")
	if !semantic {
		return nil
	}
	install, err := installer.Install(ctx, env)
	if err != nil {
		return fmt.Errorf("install semantic runtime: %w", err)
	}
	fmt.Fprintf(ioctx.Out, "support_level\t%s\nchip\t%s\n", install.SupportLevel, install.Chip)
	if install.Warning != "" {
		fmt.Fprintf(ioctx.Out, "warning\t%s\n", install.Warning)
	}
	fmt.Fprintln(ioctx.Out, "next: worktrail semantic rebuild --scope all")
	return nil
}

func parseInitSemanticFlag(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}

	hasSemantic := false
	hasNoSemantic := false
	for _, arg := range args {
		switch arg {
		case "--semantic":
			hasSemantic = true
		case "--no-semantic":
			hasNoSemantic = true
		}
	}
	if hasSemantic && hasNoSemantic {
		return false, errors.New("init flags --semantic and --no-semantic cannot be used together")
	}
	if len(args) > 1 {
		return false, fmt.Errorf("init: accepts at most one flag, got %d arguments", len(args))
	}
	switch args[0] {
	case "--semantic":
		return true, nil
	case "--no-semantic":
		return false, nil
	}
	if len(args[0]) > 0 && args[0][0] == '-' {
		return false, fmt.Errorf("init: unknown flag %q", args[0])
	}
	return false, fmt.Errorf("init: unexpected argument %q", args[0])
}
