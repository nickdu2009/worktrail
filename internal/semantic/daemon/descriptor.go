package daemon

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/util"
)

const (
	DescriptorSchema  = "worktrail.semantic.daemon.descriptor.v1"
	DescriptorVersion = 1

	stateFileName  = "state.json"
	apiKeyFileName = "api-key"
)

var (
	// ErrDescriptorNotFound reports that no descriptor has been saved yet.
	ErrDescriptorNotFound = errors.New("semantic daemon descriptor not found")

	// ErrAPIKeyExists prevents replacing an existing daemon credential.
	ErrAPIKeyExists = errors.New("semantic daemon API key already exists")
)

// Descriptor is the persistent, non-secret record for a semantic daemon.
// The API key itself is stored only in the file named by APIKeyPath.
type Descriptor struct {
	Schema          string    `json:"schema"`
	Version         int       `json:"version"`
	BundleID        string    `json:"bundle_id"`
	PID             int       `json:"pid"`
	StartTime       time.Time `json:"start_time"`
	Endpoint        string    `json:"endpoint"`
	LlamaAppVersion string    `json:"llama_app_version"`
	RuntimeSHA256   string    `json:"runtime_sha256"`
	ChipVariant     string    `json:"chip_variant"`
	ModelSHA256     string    `json:"model_sha256"`
	Alias           string    `json:"alias"`
	APIKeyPath      string    `json:"api_key_path"`
	LastSuccess     time.Time `json:"last_success"`
	LogPath         string    `json:"log_path"`
	Readiness       string    `json:"readiness"`
}

// Store manages one bundle's local daemon descriptor and API key.
type Store struct {
	bundleID  string
	runtime   string
	statePath string
	keyPath   string
	logPath   string

	fileOps *storeFileOperations
}

type storeFileOperations struct {
	remove  func(string) error
	restore func(string, []byte, os.FileMode) error
}

// NewStore constructs a Store without creating any files or directories.
func NewStore(roots paths.SemanticRoots, bundleID string) (Store, error) {
	runtime, err := roots.RuntimeState(bundleID)
	if err != nil {
		return Store{}, err
	}
	logPath, err := roots.Log(bundleID)
	if err != nil {
		return Store{}, err
	}
	statePath, err := paths.SafeJoin(runtime, stateFileName)
	if err != nil {
		return Store{}, err
	}
	keyPath, err := paths.SafeJoin(runtime, apiKeyFileName)
	if err != nil {
		return Store{}, err
	}
	return Store{
		bundleID:  bundleID,
		runtime:   runtime,
		statePath: statePath,
		keyPath:   keyPath,
		logPath:   logPath,
	}, nil
}

// StatePath returns the descriptor location managed by this Store.
func (s Store) StatePath() string {
	return s.statePath
}

// APIKeyPath returns the API key location managed by this Store.
func (s Store) APIKeyPath() string {
	return s.keyPath
}

// APIKey reads the Store-owned daemon credential without creating it.
func (s Store) APIKey() (string, error) {
	data, err := os.ReadFile(s.keyPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("semantic daemon API key is missing: %w", err)
		}
		return "", fmt.Errorf("read semantic daemon API key: %w", err)
	}
	key := string(data)
	if key == "" {
		return "", errors.New("semantic daemon API key is empty")
	}
	return key, nil
}

// Load reads the descriptor without creating its runtime directory.
func (s Store) Load() (Descriptor, error) {
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Descriptor{}, fmt.Errorf("%w: %s", ErrDescriptorNotFound, s.statePath)
		}
		return Descriptor{}, err
	}

	var descriptor Descriptor
	if err := json.Unmarshal(data, &descriptor); err != nil {
		return Descriptor{}, fmt.Errorf("decode semantic daemon descriptor: %w", err)
	}
	if err := s.validateDescriptor(descriptor); err != nil {
		return Descriptor{}, err
	}
	return descriptor, nil
}

// Save writes a descriptor atomically and records only Store-owned paths.
func (s Store) Save(descriptor Descriptor) error {
	normalized, err := s.normalizeDescriptor(descriptor)
	if err != nil {
		return err
	}
	if err := s.ensureRuntimeDir(); err != nil {
		return err
	}

	data, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("encode semantic daemon descriptor: %w", err)
	}
	if err := util.AtomicWrite(s.statePath, data, 0o600); err != nil {
		return fmt.Errorf("write semantic daemon descriptor: %w", err)
	}
	return nil
}

// GenerateAPIKey creates and persists a new random daemon credential. It never
// overwrites a pre-existing key.
func (s Store) GenerateAPIKey() (string, error) {
	if err := s.ensureRuntimeDir(); err != nil {
		return "", err
	}

	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate semantic daemon API key: %w", err)
	}
	key := base64.RawURLEncoding.EncodeToString(bytes)
	file, err := os.OpenFile(s.keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", ErrAPIKeyExists
		}
		return "", fmt.Errorf("write semantic daemon API key: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		os.Remove(s.keyPath)
		return "", fmt.Errorf("secure semantic daemon API key: %w", err)
	}
	if _, err := file.WriteString(key); err != nil {
		file.Close()
		os.Remove(s.keyPath)
		return "", fmt.Errorf("write semantic daemon API key: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(s.keyPath)
		return "", fmt.Errorf("sync semantic daemon API key: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(s.keyPath)
		return "", fmt.Errorf("close semantic daemon API key: %w", err)
	}
	return key, nil
}

// Remove deletes only this Store's descriptor and API key. It deliberately
// retains the runtime directory and every other entry under it.
func (s Store) Remove() error {
	state, err := storedFile(s.statePath)
	if err != nil {
		return err
	}
	remove := os.Remove
	restore := util.AtomicWrite
	if s.fileOps != nil {
		if s.fileOps.remove != nil {
			remove = s.fileOps.remove
		}
		if s.fileOps.restore != nil {
			restore = s.fileOps.restore
		}
	}
	if err := remove(s.statePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := remove(s.keyPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		if !state.exists {
			return err
		}
		if restoreErr := restore(s.statePath, state.data, state.mode); restoreErr != nil {
			return fmt.Errorf("remove semantic daemon API key: %w; restore descriptor: %v", err, restoreErr)
		}
		return err
	}
	return nil
}

// Quarantine moves untrusted legacy state out of the active runtime location.
// It never opens or signals the recorded PID.
func (s Store) Quarantine() error {
	if _, err := os.Lstat(s.statePath); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	root := filepath.Join(s.runtime, "quarantine")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return err
	}
	destination, err := os.MkdirTemp(root, "identity-")
	if err != nil {
		return err
	}
	quarantinedKey := filepath.Join(destination, filepath.Base(s.keyPath))
	keyMoved := false
	if err := os.Rename(s.keyPath, quarantinedKey); err == nil {
		keyMoved = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(s.statePath, filepath.Join(destination, filepath.Base(s.statePath))); err != nil {
		if keyMoved {
			if restoreErr := os.Rename(quarantinedKey, s.keyPath); restoreErr != nil {
				return fmt.Errorf("quarantine semantic daemon descriptor: %w; restore API key: %v", err, restoreErr)
			}
		}
		return err
	}
	return nil
}

type storedRuntimeFile struct {
	data   []byte
	mode   os.FileMode
	exists bool
}

func storedFile(path string) (storedRuntimeFile, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return storedRuntimeFile{}, nil
	}
	if err != nil {
		return storedRuntimeFile{}, err
	}
	if !info.Mode().IsRegular() {
		return storedRuntimeFile{}, errors.New("semantic daemon state file is not regular")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return storedRuntimeFile{}, err
	}
	return storedRuntimeFile{data: data, mode: info.Mode().Perm(), exists: true}, nil
}

func (s Store) normalizeDescriptor(descriptor Descriptor) (Descriptor, error) {
	if descriptor.Schema == "" {
		descriptor.Schema = DescriptorSchema
	}
	if descriptor.Version == 0 {
		descriptor.Version = DescriptorVersion
	}
	if descriptor.BundleID == "" {
		descriptor.BundleID = s.bundleID
	}
	if descriptor.APIKeyPath == "" {
		descriptor.APIKeyPath = s.keyPath
	}
	if descriptor.LogPath == "" {
		descriptor.LogPath = s.logPath
	}
	if err := s.validateDescriptor(descriptor); err != nil {
		return Descriptor{}, err
	}
	return descriptor, nil
}

func (s Store) validateDescriptor(descriptor Descriptor) error {
	if descriptor.Schema != DescriptorSchema {
		return fmt.Errorf("unsupported semantic daemon descriptor schema %q", descriptor.Schema)
	}
	if descriptor.Version != DescriptorVersion {
		return fmt.Errorf("unsupported semantic daemon descriptor version %d", descriptor.Version)
	}
	if descriptor.BundleID != s.bundleID {
		return errors.New("semantic daemon descriptor bundle ID does not match store")
	}
	if descriptor.APIKeyPath != s.keyPath {
		return errors.New("semantic daemon descriptor API key path is not store-owned")
	}
	if descriptor.LogPath != s.logPath {
		return errors.New("semantic daemon descriptor log path is not store-owned")
	}
	return nil
}

func (s Store) ensureRuntimeDir() error {
	if err := os.MkdirAll(s.runtime, 0o700); err != nil {
		return err
	}
	return os.Chmod(s.runtime, 0o700)
}
