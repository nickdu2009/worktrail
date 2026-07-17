package app

import (
	"fmt"
	"strings"

	"github.com/nickdu2009/worktrail/internal/contextpack"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

// ContextSemanticSelector is the app boundary for semantic context selection.
// Production composition may provide an implementation without making this
// command package depend on a semantic runtime.
type ContextSemanticSelector = contextpack.Selector

type unavailableContextSemanticSelector struct{}

func (unavailableContextSemanticSelector) Select(contextpack.SelectionRequest) ([]contextpack.Item, error) {
	return nil, &SemanticSearchError{Code: contracts.ReasonRuntimeUnavailable}
}

type contextSemanticOptions struct {
	Mode    contracts.Mode
	Enabled bool
	Args    []string
}

func parseContextSemanticOptions(args []string) (contextSemanticOptions, error) {
	options := contextSemanticOptions{Mode: contracts.ModeLexical}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--semantic":
			if options.Enabled || i+1 < len(args) && (args[i+1] == "auto" || args[i+1] == "required") {
				return contextSemanticOptions{}, contextSemanticUsageError(args)
			}
			options.Enabled = true
			options.Mode = contracts.ModeAuto
		case strings.HasPrefix(arg, "--semantic="):
			if options.Enabled {
				return contextSemanticOptions{}, contextSemanticUsageError(args)
			}
			mode, err := contracts.ParseMode(strings.TrimPrefix(arg, "--semantic="))
			if err != nil || mode == contracts.ModeLexical {
				return contextSemanticOptions{}, contextSemanticUsageError(args)
			}
			options.Enabled = true
			options.Mode = mode
		case strings.HasPrefix(arg, "--semantic"), strings.HasPrefix(arg, "--explain"):
			return contextSemanticOptions{}, contextSemanticUsageError(args)
		default:
			options.Args = append(options.Args, arg)
		}
	}
	return options, nil
}

func contextSemanticUsageError(args []string) error {
	return fmt.Errorf("usage: worktrail context [--semantic|--semantic=auto|--semantic=required] [--topic <topic>] [--stage <stage>] [--include-lifecycle <list>] [--evidence] [--format markdown|json] <task>: invalid semantic arguments %q", args)
}
