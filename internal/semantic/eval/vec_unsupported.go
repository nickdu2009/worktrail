//go:build !linux && !darwin && !freebsd && !openbsd && !windows

package eval

import (
	"context"
	"errors"
)

const VecReportSchema = "worktrail.semantic.eval.vec.v1"

type VecOptions struct {
	Path      string
	Count     int
	Dimension int
	Queries   int
	Limit     int
	Seed      uint64
}

type VecReport struct {
	Schema       string  `json:"schema"`
	VecVersion   string  `json:"vec_version"`
	Count        int     `json:"count"`
	Dimension    int     `json:"dimension"`
	Queries      int     `json:"queries"`
	Limit        int     `json:"limit"`
	DatabasePath string  `json:"database_path,omitempty"`
	DatabaseSize int64   `json:"database_size"`
	InsertMS     float64 `json:"insert_ms"`
	QueryP50MS   float64 `json:"query_p50_ms"`
	QueryP95MS   float64 `json:"query_p95_ms"`
}

func RunVecSpike(context.Context, VecOptions) (VecReport, error) {
	return VecReport{}, errors.New("sqlite-vec synthetic spike is unsupported on this platform")
}
