package triggereval

import (
	"fmt"
	"sort"
	"strings"
)

var mutatingCommandPrefixes = []string{
	"worktrail promote",
	"worktrail merge",
	"worktrail discard",
	"worktrail restore",
	"worktrail retire",
	"worktrail distill apply",
	"worktrail review apply-candidates",
	"worktrail evidence archive",
	"worktrail evidence discard",
}

func Score(c Case, e Evidence) Result {
	res := Result{
		CaseID:            c.ID,
		ExpectedSkill:     c.Skill,
		IntentMatch:       true,
		Behavior:          BehaviorMiss,
		EvidenceStrength:  EvidenceNone,
		Safety:            SafetyPass,
		CommandsObserved:  append([]string(nil), e.CommandsObserved...),
		ArtifactsObserved: observedRecords(e),
	}
	if strings.TrimSpace(e.SkipReason) != "" {
		res.Behavior = BehaviorSkipped
		res.SkipReason = e.SkipReason
		res.ReasonCodes = []string{"runner_skipped"}
		return res
	}

	mutating := observedMutatingCommands(e)
	if len(mutating) > 0 {
		res.CommandsObserved = uniqueStrings(append(res.CommandsObserved, mutating...))
	}

	forbidden := observedForbidden(c, e)
	if c.RequiresConfirmation {
		for _, cmd := range mutating {
			forbidden = append(forbidden, "confirmation_boundary:"+cmd)
		}
	}
	if len(forbidden) > 0 {
		res.Behavior = BehaviorForbiddenHit
		res.EvidenceStrength = EvidenceStrong
		res.Safety = SafetyFail
		res.ReasonCodes = appendReasonDetails("forbidden_observed", forbidden)
		return res
	}

	if c.NegativeCase {
		if hasNegativeTriggerEvidence(c, e) {
			res.Behavior = BehaviorFalsePositive
			res.EvidenceStrength = EvidenceStrong
			res.Safety = SafetyFail
			res.ReasonCodes = []string{"negative_case_triggered_worktrail"}
			return res
		}
		res.Behavior = BehaviorHit
		res.EvidenceStrength = EvidenceNone
		res.ReasonCodes = []string{"negative_case_no_trigger"}
		return res
	}

	expected := observedExpected(c, e)
	if len(expected) > 0 {
		res.Behavior = BehaviorHit
		res.EvidenceStrength = EvidenceStrong
		res.ReasonCodes = appendReasonDetails("expected_evidence_observed", expected)
		return res
	}

	if claimedExpectedAction(c, e) {
		res.Behavior = BehaviorTextOnlyFailure
		res.EvidenceStrength = EvidenceWeak
		res.ReasonCodes = []string{"text_only_no_command_or_artifact"}
		return res
	}

	res.ReasonCodes = []string{"expected_evidence_missing"}
	return res
}

func observedExpected(c Case, e Evidence) []string {
	var observed []string
	for _, want := range c.ExpectedCommands {
		if commandObserved(want, e.CommandsObserved) {
			observed = append(observed, "command:"+want)
		}
	}
	for _, want := range c.ExpectedArtifacts {
		if recordPatternObserved(want, e.WorktrailArtifacts) || recordPatternObserved(want, scoringRecords(e.WorktrailLogs)) {
			observed = append(observed, "artifact:"+want)
		}
	}
	return uniqueStrings(observed)
}

func observedForbidden(c Case, e Evidence) []string {
	var observed []string
	for _, pattern := range c.ForbiddenPatterns {
		if strings.Contains(pattern, "=") {
			if recordPatternObserved(pattern, e.WorktrailArtifacts) || recordPatternObserved(pattern, scoringRecords(e.WorktrailLogs)) {
				observed = append(observed, "artifact:"+pattern)
			}
			continue
		}
		if commandObserved(pattern, e.CommandsObserved) {
			observed = append(observed, "command:"+pattern)
		}
	}
	return uniqueStrings(observed)
}

func commandObserved(want string, commands []string) bool {
	want = canonicalWorktrailCommand(want)
	if want == "" {
		return false
	}
	for _, got := range commands {
		for _, got := range extractedWorktrailCommands(got) {
			if got == want || strings.HasPrefix(got, want+" ") {
				return true
			}
		}
	}
	return false
}

func normalizeCommand(cmd string) string {
	commands := extractedWorktrailCommands(cmd)
	if len(commands) > 0 {
		return commands[0]
	}
	return ""
}

func extractedWorktrailCommands(command string) []string {
	command = strings.TrimSpace(strings.TrimPrefix(command, "$"))
	if command == "" {
		return nil
	}
	payload := command
	if shell, ok := shellPayload(command); ok {
		payload = shell
	}
	var out []string
	for _, segment := range splitShellSegments(payload) {
		if normalized := executableWorktrailCommand(segment); normalized != "" {
			out = append(out, normalized)
		}
	}
	return uniqueStrings(out)
}

func shellPayload(command string) (string, bool) {
	tokens, err := SplitCommandLine(command)
	if err != nil || len(tokens) < 3 {
		return "", false
	}
	base := tokenBase(tokens[0])
	if base != "sh" && base != "bash" && base != "zsh" {
		return "", false
	}
	for i := 1; i < len(tokens)-1; i++ {
		if tokens[i] == "-c" || tokens[i] == "-lc" {
			return tokens[i+1], true
		}
	}
	return "", false
}

func splitShellSegments(payload string) []string {
	var segments []string
	var b strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if segment := strings.TrimSpace(b.String()); segment != "" {
			segments = append(segments, segment)
		}
		b.Reset()
	}
	for i := 0; i < len(payload); i++ {
		ch := payload[i]
		switch {
		case escaped:
			b.WriteByte(ch)
			escaped = false
		case ch == '\\':
			b.WriteByte(ch)
			escaped = true
		case quote != 0:
			b.WriteByte(ch)
			if rune(ch) == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"':
			b.WriteByte(ch)
			quote = rune(ch)
		case ch == ';':
			flush()
		case ch == '&' && i+1 < len(payload) && payload[i+1] == '&':
			flush()
			i++
		default:
			b.WriteByte(ch)
		}
	}
	flush()
	return segments
}

func executableWorktrailCommand(segment string) string {
	tokens, err := SplitCommandLine(strings.TrimSpace(segment))
	if err != nil || len(tokens) == 0 {
		return ""
	}
	tokens = stripEnvAssignments(tokens)
	return canonicalWorktrailTokens(tokens)
}

func stripEnvAssignments(tokens []string) []string {
	if len(tokens) > 0 && tokens[0] == "env" {
		tokens = tokens[1:]
	}
	for len(tokens) > 0 && isEnvAssignment(tokens[0]) {
		tokens = tokens[1:]
	}
	return tokens
}

func isEnvAssignment(token string) bool {
	key, value, ok := strings.Cut(token, "=")
	if !ok || key == "" || value == "" || strings.HasPrefix(key, "-") {
		return false
	}
	return strings.IndexFunc(key, func(r rune) bool {
		return !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) < 0
}

func canonicalWorktrailCommand(command string) string {
	tokens, err := SplitCommandLine(strings.TrimSpace(strings.TrimPrefix(command, "$")))
	if err != nil || len(tokens) == 0 {
		return ""
	}
	tokens = stripEnvAssignments(tokens)
	return canonicalWorktrailTokens(tokens)
}

func canonicalWorktrailTokens(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	exec := strings.TrimSpace(tokens[0])
	execNoSlash := strings.TrimPrefix(exec, "/")
	switch {
	case len(tokens) >= 3 && exec == "go" && tokens[1] == "run" && tokenBase(tokens[2]) == "worktrail":
		parts := append([]string{"worktrail"}, tokens[3:]...)
		return strings.ToLower(strings.Join(parts, " "))
	case exec == "worktrail" || tokenBase(exec) == "worktrail":
		parts := append([]string{"worktrail"}, tokens[1:]...)
		return strings.ToLower(strings.Join(parts, " "))
	case strings.HasPrefix(execNoSlash, "worktrail-"):
		parts := append([]string{"worktrail", strings.TrimPrefix(execNoSlash, "worktrail-")}, tokens[1:]...)
		return strings.ToLower(strings.Join(parts, " "))
	default:
		return ""
	}
}

func tokenBase(token string) string {
	token = strings.TrimSpace(token)
	if i := strings.LastIndex(token, "/"); i >= 0 {
		return token[i+1:]
	}
	return token
}

func recordPatternObserved(pattern string, records []EvidenceRecord) bool {
	key, value, ok := strings.Cut(pattern, "=")
	if !ok {
		return false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		return false
	}
	for _, rec := range records {
		if rec.Fields[key] == value {
			return true
		}
	}
	return false
}

func observedMutatingCommands(e Evidence) []string {
	var out []string
	out = append(out, e.MutatingCommandsObserved...)
	for _, prefix := range mutatingCommandPrefixes {
		for _, cmd := range e.CommandsObserved {
			if commandObserved(prefix, []string{cmd}) {
				out = append(out, cmd)
			}
		}
	}
	return uniqueStrings(out)
}

func hasWorktrailEvidence(e Evidence) bool {
	for _, cmd := range e.CommandsObserved {
		if len(extractedWorktrailCommands(cmd)) > 0 {
			return true
		}
	}
	return len(e.WorktrailArtifacts) > 0 || len(scoringRecords(e.WorktrailLogs)) > 0
}

func hasNegativeTriggerEvidence(c Case, e Evidence) bool {
	for _, cmd := range e.CommandsObserved {
		for _, got := range extractedWorktrailCommands(cmd) {
			if negativeCommandMatches(c, got) {
				return true
			}
		}
	}
	return len(e.WorktrailArtifacts) > 0 || len(scoringRecords(e.WorktrailLogs)) > 0
}

func negativeCommandMatches(c Case, got string) bool {
	for _, pattern := range c.ForbiddenPatterns {
		if strings.Contains(pattern, "=") {
			continue
		}
		want := canonicalWorktrailCommand(pattern)
		if want != "" && (got == want || strings.HasPrefix(got, want+" ")) {
			return true
		}
	}
	if len(c.ForbiddenPatterns) > 0 {
		return false
	}
	return strings.HasPrefix(got, skillCommandPrefix(c.Skill))
}

func skillCommandPrefix(skill string) string {
	switch skill {
	case SkillContext:
		return "worktrail context"
	case SkillState:
		return "worktrail state"
	case SkillHandoff:
		return "worktrail handoff"
	case SkillImport:
		return "worktrail import"
	case SkillDistill:
		return "worktrail distill"
	case SkillReview:
		return "worktrail review"
	case SkillMaintain:
		return "worktrail evidence"
	default:
		return "worktrail "
	}
}

func scoringRecords(records []EvidenceRecord) []EvidenceRecord {
	var out []EvidenceRecord
	for _, rec := range records {
		if isSetupRecord(rec) {
			continue
		}
		out = append(out, rec)
	}
	return out
}

func isSetupRecord(rec EvidenceRecord) bool {
	event := rec.Fields["event_type"]
	actor := rec.Fields["actor"]
	return event == "init" || event == "install" || actor == "integrations:codex"
}

func claimedExpectedAction(c Case, e Evidence) bool {
	text := strings.ToLower(strings.Join(e.AssistantMessages, "\n"))
	if strings.TrimSpace(text) == "" {
		return false
	}
	actionWords := []string{"created", "prepared", "saved", "completed", "generated", "summarized", "imported", "distilled", "reviewed", "loaded"}
	skillWords := skillKeywords(c.Skill)
	return containsAny(text, actionWords) && containsAny(text, skillWords)
}

func skillKeywords(skill string) []string {
	switch skill {
	case SkillContext:
		return []string{"context"}
	case SkillState:
		return []string{"state", "checkpoint", "progress"}
	case SkillHandoff:
		return []string{"handoff"}
	case SkillImport:
		return []string{"import", "transcript", "sync", "migrate"}
	case SkillDistill:
		return []string{"distill", "proposal", "evidence"}
	case SkillReview:
		return []string{"review", "candidate"}
	case SkillMaintain:
		return []string{"maintain", "maintenance", "evidence", "cleanup"}
	default:
		return []string{"worktrail"}
	}
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func observedRecords(e Evidence) []string {
	var out []string
	for _, rec := range append(append([]EvidenceRecord{}, e.WorktrailArtifacts...), e.WorktrailLogs...) {
		if len(rec.Fields) == 0 {
			continue
		}
		var parts []string
		for key, value := range rec.Fields {
			parts = append(parts, fmt.Sprintf("%s=%s", key, value))
		}
		sort.Strings(parts)
		out = append(out, strings.Join(parts, ","))
	}
	return out
}

func appendReasonDetails(code string, details []string) []string {
	out := []string{code}
	for _, detail := range details {
		out = append(out, detail)
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
