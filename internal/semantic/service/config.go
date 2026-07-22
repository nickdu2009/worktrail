package service

import (
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
	ConfigSchema       = "worktrail.semantic.service-config.v1"
	defaultIdleTimeout = 10 * time.Minute
	minimumIdleTimeout = time.Minute
	maximumIdleTimeout = 60 * time.Minute
)

type Config struct {
	Schema      string `json:"schema"`
	IdleTimeout string `json:"idle_timeout"`
}

func DefaultConfig() Config {
	return Config{Schema: ConfigSchema, IdleTimeout: defaultIdleTimeout.String()}
}

func LoadConfig(roots paths.SemanticRoots) (Config, time.Duration, error) {
	info, statErr := os.Lstat(roots.ServiceConfig())
	if statErr == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return Config{}, 0, errors.New("semantic service config must be a 0600 regular file")
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return Config{}, 0, fmt.Errorf("inspect semantic service config: %w", statErr)
	}
	data, err := os.ReadFile(roots.ServiceConfig())
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultConfig(), defaultIdleTimeout, nil
	}
	if err != nil {
		return Config{}, 0, fmt.Errorf("read semantic service config: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, 0, errors.New("decode semantic service config")
	}
	duration, err := validateConfig(config)
	if err != nil {
		return Config{}, 0, err
	}
	return config, duration, nil
}

func EnsureConfig(roots paths.SemanticRoots) error {
	if _, _, err := LoadConfig(roots); err == nil {
		if _, statErr := os.Stat(roots.ServiceConfig()); statErr == nil {
			return nil
		}
	} else {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(roots.ServiceConfig()), 0o700); err != nil {
		return fmt.Errorf("create semantic service config directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(roots.ServiceConfig()), 0o700); err != nil {
		return fmt.Errorf("secure semantic service config directory: %w", err)
	}
	data, err := json.Marshal(DefaultConfig())
	if err != nil {
		return err
	}
	if err := util.AtomicWrite(roots.ServiceConfig(), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write semantic service config: %w", err)
	}
	return nil
}

func validateConfig(config Config) (time.Duration, error) {
	if config.Schema != ConfigSchema {
		return 0, fmt.Errorf("unsupported semantic service config schema %q", config.Schema)
	}
	duration, err := time.ParseDuration(config.IdleTimeout)
	if err != nil {
		return 0, errors.New("invalid semantic service idle timeout")
	}
	if duration < minimumIdleTimeout || duration > maximumIdleTimeout {
		return 0, errors.New("semantic service idle timeout must be between 1m and 60m")
	}
	return duration, nil
}
