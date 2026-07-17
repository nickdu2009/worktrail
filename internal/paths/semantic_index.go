package paths

import (
	"errors"
	"path/filepath"
	"strings"
)

// SemanticIndexRoot returns the durable semantic index root for scope. It is
// intentionally separate from SemanticRoots, which contains evictable runtime
// cache and state.
func (e Env) SemanticIndexRoot(scope string) (string, error) {
	scopeRoot, err := e.ScopeRoot(scope)
	if err != nil {
		return "", err
	}
	return SafeJoin(scopeRoot, "index", "semantic")
}

// ActivePointerPath returns the path of the active generation pointer.
func (e Env) ActivePointerPath(scope string) (string, error) {
	root, err := e.SemanticIndexRoot(scope)
	if err != nil {
		return "", err
	}
	return SafeJoin(root, "active.json")
}

// GenerationDBPath returns the database path owned by generationID.
func (e Env) GenerationDBPath(scope, generationID string) (string, error) {
	if err := validateGenerationID(generationID); err != nil {
		return "", err
	}
	root, err := e.SemanticIndexRoot(scope)
	if err != nil {
		return "", err
	}
	return SafeJoin(root, generationID+".sqlite")
}

// CoordinationLockPath returns the lock used to coordinate index updates.
func (e Env) CoordinationLockPath(scope string) (string, error) {
	root, err := e.SemanticIndexRoot(scope)
	if err != nil {
		return "", err
	}
	return SafeJoin(root, "coordination.lock")
}

// GenerationLeasePath returns the lease path owned by generationID.
func (e Env) GenerationLeasePath(scope, generationID string) (string, error) {
	if err := validateGenerationID(generationID); err != nil {
		return "", err
	}
	root, err := e.SemanticIndexRoot(scope)
	if err != nil {
		return "", err
	}
	return SafeJoin(root, generationID+".lease")
}

func validateGenerationID(generationID string) error {
	if generationID == "" || strings.TrimSpace(generationID) != generationID ||
		generationID == "." || generationID == ".." ||
		filepath.Base(generationID) != generationID ||
		strings.ContainsAny(generationID, `/\`) {
		return errors.New("invalid semantic generation ID")
	}
	return nil
}
