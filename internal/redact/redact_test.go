package redact

import (
	"strings"
	"testing"
)

func TestScanClean(t *testing.T) {
	result := Scan("Keep a short project note without sensitive values.")
	if result.Status != StatusClean {
		t.Fatalf("status = %s, want %s", result.Status, StatusClean)
	}
	if result.Text != "Keep a short project note without sensitive values." {
		t.Fatalf("clean scan changed text: %q", result.Text)
	}
}

func TestScanRedactsSensitivePatterns(t *testing.T) {
	input := strings.Join([]string{
		"AWS key AKIA1234567890ABCDEF",
		"JWT eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.aLongSignatureValue1234567890",
		"OPENAI_API_KEY=sk-proj-abcdefghijklmnopqrstuvwxyz123456",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz1234567890",
		"postgres://writer:supersecret@localhost/db",
		"Contact jane.doe@example.com or 415-555-1212 with SSN 123-45-6789.",
	}, "\n")

	result := Scan(input)
	if result.Status != StatusRedacted {
		t.Fatalf("status = %s, want %s", result.Status, StatusRedacted)
	}
	for _, secret := range []string{
		"AKIA1234567890ABCDEF",
		"eyJhbGciOiJIUzI1NiJ9",
		"sk-proj-abcdefghijklmnopqrstuvwxyz123456",
		"abcdefghijklmnopqrstuvwxyz1234567890",
		"supersecret",
		"jane.doe@example.com",
		"415-555-1212",
		"123-45-6789",
	} {
		if strings.Contains(result.Text, secret) {
			t.Fatalf("redacted text still contains %q:\n%s", secret, result.Text)
		}
	}
	for _, marker := range []string{
		"[REDACTED:api-key]",
		"[REDACTED:jwt]",
		"[REDACTED:oauth-session-token]",
		"[REDACTED:db-password]",
		"[REDACTED:email]",
		"[REDACTED:phone]",
		"[REDACTED:ssn]",
	} {
		if !strings.Contains(result.Text, marker) {
			t.Fatalf("redacted text missing %q:\n%s", marker, result.Text)
		}
	}
	if len(result.Findings) < 7 {
		t.Fatalf("findings len = %d, want at least 7", len(result.Findings))
	}
}

func TestScanBlocksSSHPrivateKeys(t *testing.T) {
	input := "before\n-----BEGIN OPENSSH PRIVATE KEY-----\nabc123\n-----END OPENSSH PRIVATE KEY-----\nafter"
	result := Scan(input)
	if result.Status != StatusBlocked {
		t.Fatalf("status = %s, want %s", result.Status, StatusBlocked)
	}
	if result.Text != input {
		t.Fatal("blocked scan should not rewrite text")
	}
	if len(result.Findings) != 1 || result.Findings[0].Type != "ssh_private_key" {
		t.Fatalf("findings = %#v", result.Findings)
	}
}
