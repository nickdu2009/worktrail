// Package contracts holds the stable, dependency-light boundary shared by
// semantic installation, daemon, chunking, and retrieval.
package contracts

import (
	"context"
	"fmt"
)

type Mode string

const (
	ModeLexical  Mode = "lexical"
	ModeAuto     Mode = "auto"
	ModeRequired Mode = "required"
)

func (m Mode) Valid() bool {
	return m == ModeLexical || m == ModeAuto || m == ModeRequired
}

func ParseMode(value string) (Mode, error) {
	mode := Mode(value)
	if !mode.Valid() {
		return "", fmt.Errorf("invalid semantic mode %q", value)
	}
	return mode, nil
}

type ReasonCode string

const (
	ReasonPlatformUnsupported ReasonCode = "semantic_platform_unsupported"
	ReasonBundleMissing       ReasonCode = "semantic_bundle_missing"
	ReasonRuntimeUnavailable  ReasonCode = "semantic_runtime_unavailable"
	ReasonGenerationMissing   ReasonCode = "semantic_generation_missing"
	ReasonProfileStale        ReasonCode = "semantic_profile_stale"
)

// TokenCounter is implemented by the daemon client in a later step. Chunking
// depends only on this contract, never on process supervision.
type TokenCounter interface {
	CountTokens(ctx context.Context, text string) (int, error)
}
