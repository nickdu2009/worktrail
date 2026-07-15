package app

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/textsafety"
)

const cliErrorSchema = "worktrail.cli.error.v1"

type CLIErrorReport struct {
	Schema     string             `json:"schema"`
	OK         bool               `json:"ok"`
	Command    string             `json:"command"`
	Message    string             `json:"message"`
	ErrorCodes []string           `json:"error_codes,omitempty"`
	Issues     []textsafety.Issue `json:"issues,omitempty"`
}

func inferJSONMode(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--format=json" || arg == "--format" && i+1 < len(args) && args[i+1] == "json":
			return true
		case arg == "--json" || arg == "--json=true" || strings.HasPrefix(arg, "--json="):
			return true
		}
	}
	return false
}

func joinCommand(args []string) string {
	if len(args) == 0 {
		return "worktrail"
	}
	return "worktrail " + strings.Join(args, " ")
}

func buildCLIErrorReport(command string, err error) CLIErrorReport {
	command = strings.TrimSpace(command)
	if command == "" {
		command = "worktrail"
	}
	report := CLIErrorReport{
		Schema:     cliErrorSchema,
		OK:         false,
		Command:    command,
		Message:    err.Error(),
		ErrorCodes: cliErrorCodes(err),
	}
	var validationErr *textsafety.ValidationError
	if errors.As(err, &validationErr) {
		report.Issues = append([]textsafety.Issue(nil), validationErr.Issues...)
		if len(report.ErrorCodes) == 0 {
			report.ErrorCodes = validationErr.Codes()
		}
	}
	if len(report.ErrorCodes) == 0 {
		report.ErrorCodes = []string{generalCLIErrorCode(err)}
	}
	return report
}

func cliErrorCodes(err error) []string {
	if err == nil {
		return nil
	}
	var validationErr *textsafety.ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Codes()
	}
	if errors.Is(err, candidate.ErrBlocked) {
		return []string{textsafety.FieldCode("body", "blocked_sensitive_material")}
	}
	if errors.Is(err, candidate.ErrRetireReasonRequired) || errors.Is(err, candidate.ErrEvidenceReasonRequired) {
		return []string{textsafety.FieldCode("reason", "required")}
	}
	code := generalCLIErrorCode(err)
	if code == "" {
		return nil
	}
	return []string{code}
}

func generalCLIErrorCode(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, os.ErrNotExist) || strings.Contains(msg, "no such file"):
		return "cli_file_load_failed"
	case strings.Contains(msg, "scope mismatch"):
		return "cli_scope_mismatch"
	case strings.Contains(msg, "requires --confirm"):
		return "cli_confirmation_required"
	case strings.Contains(msg, "requires a candidate id"), strings.Contains(msg, "candidate not found"), strings.Contains(msg, "not evidence lifecycle eligible"):
		return "cli_candidate_not_found"
	case strings.Contains(msg, "requires"), strings.Contains(msg, "usage:"), strings.Contains(msg, "unknown"), strings.Contains(msg, "must be"):
		return "cli_usage_error"
	default:
		return "cli_command_failed"
	}
}

func writeCLIError(io IO, format, command string, err error) error {
	if !isJSONFormat(format) {
		return err
	}
	return json.NewEncoder(io.Out).Encode(buildCLIErrorReport(command, err))
}

func failCLICommand(io IO, format, command string, err error) error {
	if err == nil {
		return nil
	}
	if isJSONFormat(format) {
		if writeErr := writeCLIError(io, format, command, err); writeErr != nil {
			return writeErr
		}
		return nil
	}
	return err
}

func isJSONFormat(format string) bool {
	switch strings.TrimSpace(strings.ToLower(format)) {
	case "json", "true":
		return true
	default:
		return false
	}
}
