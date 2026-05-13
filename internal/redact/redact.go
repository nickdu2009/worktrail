package redact

import (
	"regexp"
	"sort"
	"strings"
)

type Status string

const (
	StatusClean    Status = "clean"
	StatusRedacted Status = "redacted"
	StatusBlocked  Status = "blocked"
)

type Action string

const (
	ActionRedact Action = "redact"
	ActionBlock  Action = "block"
)

type Finding struct {
	Type   string `json:"type"`
	Label  string `json:"label"`
	Action Action `json:"action"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
}

type Result struct {
	Status   Status    `json:"status"`
	Text     string    `json:"text"`
	Findings []Finding `json:"findings,omitempty"`
}

type pattern struct {
	typ         string
	label       string
	action      Action
	re          *regexp.Regexp
	replacement string
}

var blockPatterns = []pattern{
	{
		typ:    "ssh_private_key",
		label:  "ssh-private-key",
		action: ActionBlock,
		re:     regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH|RSA|DSA|EC|ED25519) PRIVATE KEY-----.*?-----END (?:OPENSSH|RSA|DSA|EC|ED25519) PRIVATE KEY-----`),
	},
}

var redactPatterns = []pattern{
	{
		typ:         "jwt",
		label:       "jwt",
		action:      ActionRedact,
		re:          regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
		replacement: "[REDACTED:jwt]",
	},
	{
		typ:         "api_key",
		label:       "api-key",
		action:      ActionRedact,
		re:          regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
		replacement: "[REDACTED:api-key]",
	},
	{
		typ:         "api_key",
		label:       "api-key",
		action:      ActionRedact,
		re:          regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{30,255}\b`),
		replacement: "[REDACTED:api-key]",
	},
	{
		typ:         "api_key",
		label:       "api-key",
		action:      ActionRedact,
		re:          regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}\b`),
		replacement: "[REDACTED:api-key]",
	},
	{
		typ:         "oauth_session_token",
		label:       "oauth-session-token",
		action:      ActionRedact,
		re:          regexp.MustCompile(`(?i)\b(Bearer|OAuth)\s+[A-Za-z0-9._~+/=-]{20,}\b`),
		replacement: "$1 [REDACTED:oauth-session-token]",
	},
	{
		typ:         "db_password",
		label:       "db-password",
		action:      ActionRedact,
		re:          regexp.MustCompile(`(?i)\b((?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis)://[^/\s:@]+:)[^@\s]+(@)`),
		replacement: "$1[REDACTED:db-password]$2",
	},
	{
		typ:         "env_secret",
		label:       "env-secret",
		action:      ActionRedact,
		re:          regexp.MustCompile(`(?i)\b([A-Z0-9_-]*(?:API[_-]?KEY|SECRET|TOKEN|PASSWORD|PASSWD|PWD|CLIENT[_-]?SECRET|SESSION[_-]?TOKEN|OAUTH[_-]?TOKEN|DB[_-]?PASSWORD|DATABASE_URL)[A-Z0-9_-]*\s*[:=]\s*['"]?)[A-Za-z0-9_./:+~=-]{8,}(['"]?)`),
		replacement: "$1[REDACTED:env-secret]$2",
	},
	{
		typ:         "pii_email",
		label:       "email",
		action:      ActionRedact,
		re:          regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
		replacement: "[REDACTED:email]",
	},
	{
		typ:         "pii_ssn",
		label:       "ssn",
		action:      ActionRedact,
		re:          regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		replacement: "[REDACTED:ssn]",
	},
	{
		typ:         "pii_phone",
		label:       "phone",
		action:      ActionRedact,
		re:          regexp.MustCompile(`\b(?:\+?1[-.\s]?)?(?:\(?\d{3}\)?[-.\s]?)\d{3}[-.\s]?\d{4}\b`),
		replacement: "[REDACTED:phone]",
	},
}

func Scan(text string) Result {
	findings := findAll(text, blockPatterns)
	if len(findings) > 0 {
		sortFindings(findings)
		return Result{Status: StatusBlocked, Text: text, Findings: findings}
	}

	redacted := text
	for _, p := range redactPatterns {
		findings = append(findings, findAll(redacted, []pattern{p})...)
		redacted = p.re.ReplaceAllString(redacted, p.replacement)
	}
	sortFindings(findings)
	if len(findings) == 0 {
		return Result{Status: StatusClean, Text: text}
	}
	return Result{Status: StatusRedacted, Text: redacted, Findings: findings}
}

func Clean(text string) bool {
	return Scan(text).Status == StatusClean
}

func Blocked(text string) bool {
	return Scan(text).Status == StatusBlocked
}

func findAll(text string, patterns []pattern) []Finding {
	var findings []Finding
	for _, p := range patterns {
		for _, loc := range p.re.FindAllStringIndex(text, -1) {
			findings = append(findings, Finding{
				Type:   p.typ,
				Label:  p.label,
				Action: p.action,
				Start:  loc[0],
				End:    loc[1],
			})
		}
	}
	return findings
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Start == findings[j].Start {
			return strings.Compare(findings[i].Type, findings[j].Type) < 0
		}
		return findings[i].Start < findings[j].Start
	})
}
