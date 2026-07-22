package service

import (
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
)

const (
	ProtocolVersion = 1
	HostBuildID     = "worktrail-semantic-host-v1"
	maxBodyBytes    = 1 << 20
)

type lifecycleRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	RequestID       string `json:"request_id"`
	BundleID        string `json:"bundle_id"`
}

type tokenizeRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	RequestID       string `json:"request_id"`
	BundleID        string `json:"bundle_id"`
	Text            string `json:"text"`
}

type embeddingsRequest struct {
	ProtocolVersion int      `json:"protocol_version"`
	RequestID       string   `json:"request_id"`
	BundleID        string   `json:"bundle_id"`
	Inputs          []string `json:"inputs"`
}

type response struct {
	ProtocolVersion    int                  `json:"protocol_version"`
	HostBuildID        string               `json:"host_build_id"`
	BundleID           string               `json:"bundle_id,omitempty"`
	RuntimeFingerprint string               `json:"runtime_fingerprint,omitempty"`
	Report             daemon.Report        `json:"report,omitempty"`
	TokenCount         int                  `json:"token_count,omitempty"`
	Embeddings         []daemon.Embedding   `json:"embeddings,omitempty"`
	ErrorCode          contracts.ReasonCode `json:"error_code,omitempty"`
}
