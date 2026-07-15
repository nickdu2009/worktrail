package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/paths"
)

type opsDoctorReport struct {
	Schema      string          `json:"schema"`
	OK          bool            `json:"ok"`
	Scope       string          `json:"scope"`
	Repair      bool            `json:"repair"`
	Lock        *ops.LockStatus `json:"lock,omitempty"`
	LockRemoved bool            `json:"lock_removed,omitempty"`
	Pending     []string        `json:"pending,omitempty"`
	Replayed    []string        `json:"replayed,omitempty"`
	Remaining   []string        `json:"remaining,omitempty"`
	Blocked     bool            `json:"blocked,omitempty"`
	Message     string          `json:"message,omitempty"`
}

func runDoctorOps(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	for _, arg := range args {
		if isHelpArg(arg) {
			printOpsDoctorHelp(ioctx.Out)
			return nil
		}
	}
	flags, positional := splitFlagsWithBooleans(args, map[string]bool{
		"repair":  true,
		"confirm": true,
		"json":    true,
	})
	format := flagValue(flags, "format", "text")
	if flags["json"] == "true" {
		format = "json"
	}
	fail := func(err error) error {
		return failCLICommand(ioctx, format, "worktrail doctor ops", err)
	}
	for key := range flags {
		switch key {
		case "scope", "format", "repair", "confirm", "json":
		default:
			return fail(fmt.Errorf("unknown doctor ops flag --%s", key))
		}
	}
	if format != "text" && format != "json" {
		return fail(errors.New("doctor ops format must be text or json"))
	}
	repair := flags["repair"] == "true"
	if len(positional) > 1 {
		return fail(errors.New("usage: worktrail doctor ops [status|repair] [--confirm] [--scope project|user] [--format text|json]"))
	}
	if len(positional) == 1 {
		switch positional[0] {
		case "status":
		case "repair":
			repair = true
		default:
			return fail(fmt.Errorf("unknown doctor ops action %q", positional[0]))
		}
	}
	if repair && flags["confirm"] != "true" {
		return fail(errors.New("worktrail doctor ops repair requires --confirm"))
	}
	select {
	case <-ctx.Done():
		return fail(ctx.Err())
	default:
	}
	scope := strings.TrimSpace(flagValue(flags, "scope", "project"))
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return fail(err)
	}
	report := opsDoctorReport{
		Schema: "worktrail.ops.doctor.v1",
		OK:     true,
		Scope:  scope,
		Repair: repair,
	}
	report.Pending, err = pendingOperationIDs(root)
	if err != nil {
		return fail(err)
	}
	lockStatus, lockErr := ops.InspectLock(root)
	if lockErr == nil {
		report.Lock = &lockStatus
	} else if !errors.Is(lockErr, os.ErrNotExist) {
		return fail(lockErr)
	}
	if repair {
		if report.Lock != nil {
			if report.Lock.Stale && report.Lock.Recoverable {
				if err := ops.RemoveStaleLock(root); err != nil {
					return fail(err)
				}
				report.LockRemoved = true
			} else {
				report.OK = false
				report.Blocked = true
				report.Message = "operation lock is not safely recoverable on this host; no lock or journal state was changed"
				report.Remaining = append([]string(nil), report.Pending...)
				return printOpsDoctorReport(ioctx.Out, format, report)
			}
		}
		report.Replayed, err = ops.New(root).ReplayPending()
		if err != nil {
			return fail(err)
		}
		report.Remaining, err = pendingOperationIDs(root)
		if err != nil {
			return fail(err)
		}
	}
	return printOpsDoctorReport(ioctx.Out, format, report)
}

func pendingOperationIDs(root string) ([]string, error) {
	journal := filepath.Join(root, "ops", "journal")
	entries, err := os.ReadDir(journal)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".intent.json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".intent.json")
		committed := filepath.Join(journal, id+".commit.json")
		aborted := filepath.Join(journal, id+".abort.json")
		if _, err := os.Stat(committed); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if _, err := os.Stat(aborted); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func printOpsDoctorReport(out io.Writer, format string, report opsDoctorReport) error {
	if format == "json" {
		return json.NewEncoder(out).Encode(report)
	}
	fmt.Fprintf(out, "ops scope=%s mode=%s pending=%d\n", report.Scope, opsDoctorMode(report.Repair), len(report.Pending))
	if report.Lock == nil {
		fmt.Fprintln(out, "lock=none")
	} else {
		fmt.Fprintf(out, "lock operation=%s host=%s pid=%d stale=%t recoverable=%t reason=%s\n",
			report.Lock.Owner.OperationID,
			report.Lock.Owner.Host,
			report.Lock.Owner.PID,
			report.Lock.Stale,
			report.Lock.Recoverable,
			report.Lock.Reason,
		)
	}
	if report.LockRemoved {
		fmt.Fprintln(out, "lock_removed=true")
	}
	for _, id := range report.Pending {
		fmt.Fprintf(out, "pending\t%s\n", id)
	}
	for _, id := range report.Replayed {
		fmt.Fprintf(out, "replayed\t%s\n", id)
	}
	if report.Blocked {
		fmt.Fprintln(out, "blocked:", report.Message)
	}
	return nil
}

func opsDoctorMode(repair bool) string {
	if repair {
		return "repair"
	}
	return "status"
}

func printOpsDoctorHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail doctor ops [status] [--scope project|user] [--format text|json]")
	fmt.Fprintln(out, "       worktrail doctor ops repair --confirm [--scope project|user] [--format text|json]")
	fmt.Fprintln(out, "       worktrail doctor ops --repair --confirm [--scope project|user] [--format text|json]")
	fmt.Fprintln(out, "Status is read-only. Repair only clears a recoverable same-host stale lock and replays pending journal operations.")
}
