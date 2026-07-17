package hookconfig

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCursorEmptyInstallRoundTrip(t *testing.T) {
	out, err := Reconcile(HostCursor, nil, ModeInstall)
	if err != nil {
		t.Fatal(err)
	}
	assertContainsCommand(t, out, "worktrail hook cursor beforeShellExecution")
	assertTimeout(t, out, HostCursor, "beforeShellExecution", 1)
	assertTimeout(t, out, HostCursor, "sessionStart", 2)

	removed, err := Reconcile(HostCursor, out, ModeUninstall)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(removed), "worktrail hook cursor") {
		t.Fatalf("uninstall left managed handlers: %s", removed)
	}
}

func TestCursorPreservesUserHandlers(t *testing.T) {
	current := []byte(`{
  "version": 1,
  "hooks": {
    "sessionStart": [
      {"command": "echo user-start"}
    ],
    "stop": [
      {"command": "echo user-stop"}
    ]
  }
}`)
	out, err := Reconcile(HostCursor, current, ModeInstall)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "echo user-start") || !strings.Contains(string(out), "echo user-stop") {
		t.Fatalf("user handlers not preserved: %s", out)
	}
	if !strings.Contains(string(out), "worktrail hook cursor sessionStart") {
		t.Fatalf("managed handler missing: %s", out)
	}
	// Reinstall must not duplicate.
	again, err := Reconcile(HostCursor, out, ModeInstall)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(again), "worktrail hook cursor sessionStart"); count != 1 {
		t.Fatalf("duplicate managed handlers: count=%d body=%s", count, again)
	}
}

func TestCodexLegacyWorktrailScalarUpgrades(t *testing.T) {
	current := []byte(`{
  "hooks": {
    "SessionStart": "worktrail hook codex SessionStart",
    "Stop": "worktrail hook codex Stop"
  }
}`)
	out, err := Reconcile(HostCodex, current, ModeInstall)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	hooks := doc["hooks"].(map[string]any)
	if _, ok := hooks["SessionStart"].(string); ok {
		t.Fatalf("scalar not upgraded: %s", out)
	}
	assertTimeout(t, out, HostCodex, "PreToolUse", 1)
	assertTimeout(t, out, HostCodex, "Stop", 2)
}

func TestCodexLegacyUserScalarRejected(t *testing.T) {
	current := []byte(`{
  "hooks": {
    "Stop": "my-custom-hook.sh"
  }
}`)
	_, err := Reconcile(HostCodex, current, ModeInstall)
	if !errors.Is(err, ErrLegacyCodexUserHook) {
		t.Fatalf("err=%v want ErrLegacyCodexUserHook", err)
	}
}

func TestCodexUninstallPreservesNonWorktrailScalarAndRemovesManaged(t *testing.T) {
	current := []byte(`{
  "hooks": {
    "Stop": "my-custom-hook.sh",
    "SessionStart": [
      {
        "hooks": [
          {"type":"command","command":"worktrail hook codex SessionStart","timeout":2},
          {"type":"command","command":"echo user-start","timeout":2}
        ]
      }
    ]
  }
}`)
	out, err := Reconcile(HostCodex, current, ModeUninstall)
	if err != nil {
		t.Fatalf("uninstall should not fail on non-Worktrail scalar: %v", err)
	}
	if !strings.Contains(string(out), `"Stop": "my-custom-hook.sh"`) && !strings.Contains(string(out), `"Stop":"my-custom-hook.sh"`) {
		// Stable indent uses spaces; accept either spacing.
		if !strings.Contains(string(out), "my-custom-hook.sh") {
			t.Fatalf("user scalar not preserved: %s", out)
		}
	}
	if strings.Contains(string(out), "worktrail hook codex") {
		t.Fatalf("managed handlers should be removed: %s", out)
	}
	if !strings.Contains(string(out), "echo user-start") {
		t.Fatalf("user handler should remain: %s", out)
	}
}

func TestCodexMalformedJSONRejected(t *testing.T) {
	_, err := Reconcile(HostCodex, []byte(`{`), ModeInstall)
	if err == nil {
		t.Fatal("expected malformed JSON error")
	}
}

func TestCodexPreservesUserMatcherGroups(t *testing.T) {
	current := []byte(`{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type":"command","command":"echo user-bash","timeout":5}
        ]
      }
    ]
  }
}`)
	out, err := Reconcile(HostCodex, current, ModeInstall)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "echo user-bash") {
		t.Fatalf("user matcher not preserved: %s", out)
	}
	if !strings.Contains(string(out), `"matcher": ".*"`) {
		t.Fatalf("managed matcher missing: %s", out)
	}
	removed, err := Reconcile(HostCodex, out, ModeUninstall)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(removed), "worktrail hook codex") {
		t.Fatalf("uninstall left managed handlers: %s", removed)
	}
	if !strings.Contains(string(removed), "echo user-bash") {
		t.Fatalf("uninstall removed user handler: %s", removed)
	}
}

func TestGoldenEmptyInstall(t *testing.T) {
	for _, host := range []string{HostCursor, HostCodex} {
		out, err := Reconcile(host, nil, ModeInstall)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		stable, err := StableJSON(out)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stable, "worktrail hook "+host) {
			t.Fatalf("%s golden missing managed command:\n%s", host, stable)
		}
		if host == HostCodex && !strings.Contains(stable, `"timeout"`) {
			t.Fatalf("codex handlers must include timeout:\n%s", stable)
		}
	}
}

func assertContainsCommand(t *testing.T, body []byte, command string) {
	t.Helper()
	if !strings.Contains(string(body), command) {
		t.Fatalf("missing %q in:\n%s", command, body)
	}
}

func assertTimeout(t *testing.T, body []byte, host, event string, want int) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	hooks, _ := doc["hooks"].(map[string]any)
	command := ManagedCommand(host, event)
	switch host {
	case HostCursor:
		for _, item := range asObjectSlice(hooks[event]) {
			if commandOf(item) == command {
				if timeoutOf(item) != want {
					t.Fatalf("%s timeout=%d want=%d", event, timeoutOf(item), want)
				}
				return
			}
		}
	case HostCodex:
		for _, group := range asObjectSlice(hooks[event]) {
			for _, item := range asObjectSlice(group["hooks"]) {
				if commandOf(item) == command {
					if timeoutOf(item) != want {
						t.Fatalf("%s timeout=%d want=%d", event, timeoutOf(item), want)
					}
					return
				}
			}
		}
	}
	t.Fatalf("handler not found for %s %s", host, event)
}
