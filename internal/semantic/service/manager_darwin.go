//go:build darwin

package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/nickdu2009/worktrail/internal/util"
)

func (m Manager) Install(ctx context.Context, executable string) error {
	_, err := m.InstallReversible(ctx, executable)
	return err
}

// InstallReversible installs the registration and returns a rollback that
// restores the exact previous Worktrail-owned registration.
func (m Manager) InstallReversible(ctx context.Context, executable string) (func(context.Context) error, error) {
	if !filepath.IsAbs(executable) {
		return nil, serviceError("semantic_service_unavailable", "semantic service executable must be absolute", nil)
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		return nil, serviceError("semantic_service_unavailable", "semantic service executable is unavailable", err)
	}
	if err := m.ensureGUI(ctx); err != nil {
		return nil, err
	}
	if err := EnsureConfig(m.Roots); err != nil {
		return nil, serviceError("semantic_service_unavailable", "semantic service config is unavailable", err)
	}
	if err := os.MkdirAll(m.Roots.Logs, 0o700); err != nil {
		return nil, serviceError("semantic_service_unavailable", "create semantic service log directory", err)
	}
	if err := os.Chmod(m.Roots.Logs, 0o700); err != nil {
		return nil, serviceError("semantic_service_unavailable", "secure semantic service log directory", err)
	}
	plist := launchAgentPlist(m.Label, executable, filepath.Join(m.Roots.Logs, "semantic-host.log"), m.Environment)
	metadata := m.metadataFor(executable, plist)
	previousPlist, hadPlist, err := readOptional(m.PlistPath)
	if err != nil {
		return nil, serviceError("semantic_service_unavailable", "read semantic service plist", err)
	}
	previousMetadata, hadMetadata, err := readOptional(m.Roots.ServiceMetadata())
	if err != nil {
		return nil, serviceError("semantic_service_unavailable", "read semantic service metadata", err)
	}
	if hadPlist != hadMetadata {
		return nil, serviceError("semantic_service_incompatible", "semantic service registration is incomplete", nil)
	}
	if hadPlist {
		if err := validateRegistrationFile(m.PlistPath, 0o644, m.UID); err != nil {
			return nil, serviceError("semantic_service_incompatible", "semantic service plist ownership is incompatible", err)
		}
		if err := validateRegistrationFile(m.Roots.ServiceMetadata(), 0o600, m.UID); err != nil {
			return nil, serviceError("semantic_service_incompatible", "semantic service metadata ownership is incompatible", err)
		}
		current, err := m.loadMetadata()
		if err != nil || validateInstalled(current, previousPlist) != nil {
			return nil, serviceError("semantic_service_incompatible", "semantic service registration is not Worktrail-owned", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(m.PlistPath), 0o700); err != nil {
		return nil, serviceError("semantic_service_unavailable", "create LaunchAgents directory", err)
	}
	if err := writeFile(m.PlistPath, plist, 0o644); err != nil {
		return nil, serviceError("semantic_service_unavailable", "write semantic service plist", err)
	}
	if err := m.writeMetadata(metadata); err != nil {
		_ = restoreFile(m.PlistPath, previousPlist, hadPlist, 0o644)
		return nil, serviceError("semantic_service_unavailable", "write semantic service metadata", err)
	}
	_ = m.command(ctx, "bootout", m.target())
	if err := m.command(ctx, "bootstrap", m.domain(), m.PlistPath); err != nil {
		_ = restoreFile(m.PlistPath, previousPlist, hadPlist, 0o644)
		_ = restoreFile(m.Roots.ServiceMetadata(), previousMetadata, hadMetadata, 0o600)
		if hadPlist {
			_ = m.command(ctx, "bootstrap", m.domain(), m.PlistPath)
		}
		return nil, serviceError("semantic_service_unavailable", "register semantic service", err)
	}
	rollback := func(rollbackCtx context.Context) error {
		_ = m.command(rollbackCtx, "bootout", m.target())
		if err := restoreFile(m.PlistPath, previousPlist, hadPlist, 0o644); err != nil {
			return err
		}
		if err := restoreFile(m.Roots.ServiceMetadata(), previousMetadata, hadMetadata, 0o600); err != nil {
			return err
		}
		if hadPlist {
			if err := m.command(rollbackCtx, "bootstrap", m.domain(), m.PlistPath); err != nil {
				return err
			}
		}
		return nil
	}
	return rollback, nil
}

func (m Manager) Inspect(ctx context.Context) (Inspection, error) {
	plist, hasPlist, err := readOptional(m.PlistPath)
	if err != nil {
		return Inspection{}, serviceError("semantic_service_unavailable", "read semantic service plist", err)
	}
	_, hasMetadata, err := readOptional(m.Roots.ServiceMetadata())
	if err != nil {
		return Inspection{}, serviceError("semantic_service_unavailable", "read semantic service metadata", err)
	}
	inspection := Inspection{Installed: hasPlist || hasMetadata, Domain: m.domain()}
	if !inspection.Installed {
		return inspection, nil
	}
	if !hasPlist || !hasMetadata {
		return inspection, serviceError("semantic_service_incompatible", "semantic service registration is incomplete", nil)
	}
	if err := validateRegistrationFile(m.PlistPath, 0o644, m.UID); err != nil {
		return inspection, serviceError("semantic_service_incompatible", "semantic service plist ownership is incompatible", err)
	}
	if err := validateRegistrationFile(m.Roots.ServiceMetadata(), 0o600, m.UID); err != nil {
		return inspection, serviceError("semantic_service_incompatible", "semantic service metadata ownership is incompatible", err)
	}
	metadata, err := m.loadMetadata()
	if err != nil || validateInstalled(metadata, plist) != nil {
		return inspection, serviceError("semantic_service_incompatible", "semantic service registration is not Worktrail-owned", err)
	}
	inspection.Compatible = true
	inspection.Executable = metadata.Executable
	inspection.Loaded = m.command(ctx, "print", m.target()) == nil
	return inspection, nil
}

func (m Manager) Activate(ctx context.Context) error {
	inspection, err := m.Inspect(ctx)
	if err != nil {
		return err
	}
	if !inspection.Installed {
		return serviceError("semantic_service_not_installed", "semantic service is not installed", nil)
	}
	if err := m.ensureGUI(ctx); err != nil {
		return err
	}
	if !inspection.Loaded {
		if err := m.command(ctx, "bootstrap", m.domain(), m.PlistPath); err != nil {
			return serviceError("semantic_service_unavailable", "register semantic service", err)
		}
	}
	if err := m.command(ctx, "kickstart", m.target()); err != nil {
		return serviceError("semantic_service_unavailable", "activate semantic service", err)
	}
	return nil
}

func (m Manager) Restart(ctx context.Context) error {
	inspection, err := m.Inspect(ctx)
	if err != nil {
		return err
	}
	if !inspection.Installed {
		return serviceError("semantic_service_not_installed", "semantic service is not installed", nil)
	}
	if err := m.command(ctx, "kickstart", "-k", m.target()); err != nil {
		return serviceError("semantic_service_unavailable", "restart semantic service", err)
	}
	return nil
}

func (m Manager) Remove(ctx context.Context) error {
	inspection, err := m.Inspect(ctx)
	if err != nil {
		return err
	}
	if !inspection.Installed {
		return nil
	}
	_ = m.command(ctx, "bootout", m.target())
	for _, path := range []string{m.PlistPath, m.Roots.ServiceMetadata()} {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return serviceError("semantic_service_unavailable", "remove semantic service registration", err)
		}
	}
	if err := waitRemoveOwnedSocket(ctx, m.Roots.ServiceSocket(), m.UID); err != nil {
		return serviceError("semantic_service_unavailable", "remove semantic service socket", err)
	}
	return nil
}

func waitRemoveOwnedSocket(ctx context.Context, path string, uid int) error {
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := removeOwnedSocket(path, uid)
		if err == nil || !errors.Is(err, errSocketActive) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return err
		case <-ticker.C:
		}
	}
}

func (m Manager) ensureGUI(ctx context.Context) error {
	if err := m.command(ctx, "print", m.domain()); err != nil {
		return serviceError("semantic_platform_unsupported", "semantic runtime requires a macOS GUI login session", err)
	}
	return nil
}

func (m Manager) command(ctx context.Context, args ...string) error {
	if m.run != nil {
		return m.run(ctx, args...)
	}
	return runLaunchctl(ctx, args...)
}

func runLaunchctl(ctx context.Context, args ...string) error {
	return exec.CommandContext(ctx, "/bin/launchctl", args...).Run()
}

func launchAgentPlist(label, executable, logPath string, environment map[string]string) []byte {
	var escapedLabel, escapedExecutable, escapedLogPath bytes.Buffer
	_ = xml.EscapeText(&escapedLabel, []byte(label))
	_ = xml.EscapeText(&escapedExecutable, []byte(executable))
	_ = xml.EscapeText(&escapedLogPath, []byte(logPath))
	var environmentXML bytes.Buffer
	if len(environment) != 0 {
		keys := make([]string, 0, len(environment))
		for key := range environment {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		environmentXML.WriteString("<key>EnvironmentVariables</key><dict>")
		for _, key := range keys {
			var escapedKey, escapedValue bytes.Buffer
			_ = xml.EscapeText(&escapedKey, []byte(key))
			_ = xml.EscapeText(&escapedValue, []byte(environment[key]))
			fmt.Fprintf(&environmentXML, "<key>%s</key><string>%s</string>", escapedKey.String(), escapedValue.String())
		}
		environmentXML.WriteString("</dict>")
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>semantic</string><string>host</string><string>--launchd</string></array>
<key>StandardOutPath</key><string>%s</string>
<key>StandardErrorPath</key><string>%s</string>
%s
<key>RunAtLoad</key><false/>
<key>KeepAlive</key><false/>
<key>ProcessType</key><string>Standard</string>
<key>ExitTimeOut</key><integer>10</integer>
<key>AbandonProcessGroup</key><false/>
</dict></plist>
`, escapedLabel.String(), escapedExecutable.String(), escapedLogPath.String(), escapedLogPath.String(), environmentXML.String()))
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	return restoreFile(path, data, true, mode)
}

func restoreFile(path string, data []byte, exists bool, mode os.FileMode) error {
	if !exists {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return util.AtomicWrite(path, data, mode)
}

func validateRegistrationFile(path string, mode os.FileMode, uid int) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != mode {
		return errors.New("semantic service registration file type or mode is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid {
		return errors.New("semantic service registration file owner is unsafe")
	}
	return nil
}
