// Package generation coordinates immutable semantic index generations.
package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	PointerSchema  = "worktrail.semantic.generation.pointer"
	PointerVersion = 1

	activePointerName = "active.json"
	scopeLeaseName    = ".scope.lock"
)

var ErrNoActivePointer = errors.New("semantic generation active pointer does not exist")

// Pointer identifies the immutable generation currently active for one scope.
type Pointer struct {
	Schema             string    `json:"schema"`
	Version            int       `json:"version"`
	Scope              string    `json:"scope"`
	GenerationID       string    `json:"generation_id"`
	RecallProfileID    string    `json:"recall_profile_id"`
	BundleID           string    `json:"bundle_id"`
	SnapshotHash       string    `json:"snapshot_hash"`
	ActivatedAt        time.Time `json:"activated_at"`
	RetireGenerationID string    `json:"retire_generation_id"`
}

// ActivationCandidate contains the verified immutable identity to activate.
// Activate obtains it only through a CandidateValidator.
type ActivationCandidate struct {
	Scope           string
	GenerationID    string
	RecallProfileID string
	BundleID        string
	SnapshotHash    string
}

// CandidateValidator must finish validating the candidate before activation
// takes the scope coordination lock.
type CandidateValidator func(context.Context) (ActivationCandidate, error)

// ReadActive reads and validates the active pointer in semanticDir.
func ReadActive(semanticDir string) (Pointer, error) {
	path, err := activePointerPath(semanticDir)
	if err != nil {
		return Pointer{}, err
	}
	pointer, err := readPointer(path)
	if errors.Is(err, os.ErrNotExist) {
		return Pointer{}, ErrNoActivePointer
	}
	return pointer, err
}

func (p Pointer) validate() error {
	if p.Schema != PointerSchema {
		return errors.New("unsupported semantic generation pointer schema")
	}
	if p.Version != PointerVersion {
		return errors.New("unsupported semantic generation pointer version")
	}
	if err := validateScope(p.Scope); err != nil {
		return err
	}
	if err := validateGenerationID(p.GenerationID); err != nil {
		return fmt.Errorf("invalid generation ID: %w", err)
	}
	if err := validateIdentity("recall profile ID", p.RecallProfileID); err != nil {
		return err
	}
	if err := validateIdentity("bundle ID", p.BundleID); err != nil {
		return err
	}
	if err := validateIdentity("snapshot hash", p.SnapshotHash); err != nil {
		return err
	}
	if p.ActivatedAt.IsZero() {
		return errors.New("semantic generation pointer activation time is required")
	}
	if p.RetireGenerationID != "" {
		if err := validateGenerationID(p.RetireGenerationID); err != nil {
			return fmt.Errorf("invalid retire generation ID: %w", err)
		}
		if p.RetireGenerationID == p.GenerationID {
			return errors.New("semantic generation cannot retire itself")
		}
	}
	return nil
}

func (c ActivationCandidate) pointer(activatedAt time.Time) (Pointer, error) {
	pointer := Pointer{
		Schema:          PointerSchema,
		Version:         PointerVersion,
		Scope:           c.Scope,
		GenerationID:    c.GenerationID,
		RecallProfileID: c.RecallProfileID,
		BundleID:        c.BundleID,
		SnapshotHash:    c.SnapshotHash,
		ActivatedAt:     activatedAt,
	}
	if err := pointer.validate(); err != nil {
		return Pointer{}, err
	}
	return pointer, nil
}

func readPointer(path string) (Pointer, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Pointer{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Pointer{}, errors.New("semantic generation pointer is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Pointer{}, err
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var pointer Pointer
	if err := decoder.Decode(&pointer); err != nil {
		return Pointer{}, fmt.Errorf("decode semantic generation pointer: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return Pointer{}, errors.New("semantic generation pointer contains multiple values")
		}
		return Pointer{}, fmt.Errorf("decode semantic generation pointer trailing data: %w", err)
	}
	if err := pointer.validate(); err != nil {
		return Pointer{}, err
	}
	return pointer, nil
}

func writePointer(semanticDir string, pointer Pointer) error {
	if err := pointer.validate(); err != nil {
		return err
	}
	directory, err := resolveSemanticDir(semanticDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create semantic generation directory: %w", err)
	}
	data, err := json.Marshal(pointer)
	if err != nil {
		return fmt.Errorf("encode semantic generation pointer: %w", err)
	}

	temporary, err := os.CreateTemp(directory, "."+activePointerName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create semantic generation pointer temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure semantic generation pointer temporary file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write semantic generation pointer: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync semantic generation pointer: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close semantic generation pointer: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(directory, activePointerName)); err != nil {
		return fmt.Errorf("publish semantic generation pointer: %w", err)
	}
	cleanup = false
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync semantic generation directory: %w", err)
	}
	return nil
}

func activePointerPath(semanticDir string) (string, error) {
	directory, err := resolveSemanticDir(semanticDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, activePointerName), nil
}

func generationPath(semanticDir, generationID, suffix string) (string, error) {
	if err := validateGenerationID(generationID); err != nil {
		return "", fmt.Errorf("invalid generation ID: %w", err)
	}
	directory, err := resolveSemanticDir(semanticDir)
	if err != nil {
		return "", err
	}
	target := filepath.Join(directory, generationID+suffix)
	relative, err := filepath.Rel(directory, target)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("semantic generation path escapes its directory")
	}
	return target, nil
}

func resolveSemanticDir(semanticDir string) (string, error) {
	if strings.TrimSpace(semanticDir) == "" {
		return "", errors.New("semantic generation directory is required")
	}
	directory, err := filepath.Abs(semanticDir)
	if err != nil {
		return "", fmt.Errorf("resolve semantic generation directory: %w", err)
	}
	return filepath.Clean(directory), nil
}

func validateGenerationID(generationID string) error {
	if generationID == "" || strings.TrimSpace(generationID) != generationID ||
		filepath.Base(generationID) != generationID || strings.ContainsAny(generationID, `/\`) ||
		generationID == "." || generationID == ".." {
		return errors.New("must be a non-empty filename")
	}
	return nil
}

func validateScope(scope string) error {
	if scope != "user" && scope != "project" {
		return errors.New("semantic generation scope must be user or project")
	}
	return nil
}

func validateIdentity(name, value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("semantic generation %s is required", name)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
