package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
	"github.com/nickdu2009/worktrail/internal/util"
)

const (
	DefaultLabel   = "com.nickdu2009.worktrail.semantic"
	metadataSchema = "worktrail.semantic.service-metadata.v1"
)

type Manager struct {
	Roots     paths.SemanticRoots
	Label     string
	UID       int
	PlistPath string
	// Environment carries only deterministic service roots. Production pins
	// HOME to the discovered user home; focused launchd tests replace it with
	// an isolated temporary home.
	Environment map[string]string
	run         func(context.Context, ...string) error
}

type Inspection struct {
	Installed  bool
	Loaded     bool
	Compatible bool
	Domain     string
	Executable string
}

type serviceMetadata struct {
	Schema      string `json:"schema"`
	Label       string `json:"label"`
	Executable  string `json:"executable"`
	PlistSHA256 string `json:"plist_sha256"`
}

func NewManager(roots paths.SemanticRoots) (Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Manager{}, err
	}
	return Manager{
		Roots:       roots,
		Label:       DefaultLabel,
		UID:         os.Getuid(),
		PlistPath:   filepath.Join(home, "Library", "LaunchAgents", DefaultLabel+".plist"),
		Environment: map[string]string{"HOME": home},
		run:         runLaunchctl,
	}, nil
}

func (m Manager) domain() string {
	return fmt.Sprintf("gui/%d", m.UID)
}

func (m Manager) target() string {
	return m.domain() + "/" + m.Label
}

func (m Manager) metadataFor(executable string, plist []byte) serviceMetadata {
	digest := sha256.Sum256(plist)
	return serviceMetadata{
		Schema:      metadataSchema,
		Label:       m.Label,
		Executable:  executable,
		PlistSHA256: hex.EncodeToString(digest[:]),
	}
}

func (m Manager) loadMetadata() (serviceMetadata, error) {
	data, err := os.ReadFile(m.Roots.ServiceMetadata())
	if err != nil {
		return serviceMetadata{}, err
	}
	var metadata serviceMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return serviceMetadata{}, errors.New("decode semantic service metadata")
	}
	if metadata.Schema != metadataSchema || metadata.Label != m.Label || !filepath.IsAbs(metadata.Executable) {
		return serviceMetadata{}, errors.New("semantic service metadata is incompatible")
	}
	return metadata, nil
}

func (m Manager) writeMetadata(metadata serviceMetadata) error {
	if err := os.MkdirAll(filepath.Dir(m.Roots.ServiceMetadata()), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(m.Roots.ServiceMetadata()), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return util.AtomicWrite(m.Roots.ServiceMetadata(), append(data, '\n'), 0o600)
}

func validateInstalled(metadata serviceMetadata, plist []byte) error {
	digest := sha256.Sum256(plist)
	if metadata.PlistSHA256 != hex.EncodeToString(digest[:]) {
		return errors.New("semantic service plist does not match Worktrail metadata")
	}
	return nil
}

func readOptional(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return data, err == nil, err
}

func serviceError(code contracts.ReasonCode, message string, err error) error {
	return &daemon.Error{Code: code, Message: message, Err: err}
}
