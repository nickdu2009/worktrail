package triggereval

import "testing"

func TestScoreHitFromCommand(t *testing.T) {
	c := Case{ID: "hit", Tool: ToolCodex, Skill: SkillContext, ExpectedCommands: []string{"worktrail context"}}
	result := Score(c, Evidence{CommandsObserved: []string{"worktrail context improve import"}})
	if result.Behavior != BehaviorHit || result.EvidenceStrength != EvidenceStrong {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreHitFromSlashCommand(t *testing.T) {
	c := Case{ID: "slash", Tool: ToolCodex, Skill: SkillHandoff, ExpectedCommands: []string{"worktrail handoff"}}
	result := Score(c, Evidence{CommandsObserved: []string{"/worktrail-handoff next chat"}})
	if result.Behavior != BehaviorHit || result.EvidenceStrength != EvidenceStrong {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreHitFromEnvPrefixedCommand(t *testing.T) {
	c := Case{ID: "env", Tool: ToolCodex, Skill: SkillContext, ExpectedCommands: []string{"worktrail context"}}
	result := Score(c, Evidence{CommandsObserved: []string{`WORKTRAIL_HOME="$PWD/.x" worktrail context "task"`}})
	if result.Behavior != BehaviorHit || result.EvidenceStrength != EvidenceStrong {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreHitFromShellWrappedChainedCommand(t *testing.T) {
	c := Case{ID: "shell", Tool: ToolCodex, Skill: SkillState, ExpectedCommands: []string{"worktrail state"}}
	result := Score(c, Evidence{CommandsObserved: []string{`/bin/zsh -lc 'mkdir -p x && WORKTRAIL_HOME=.x worktrail state start title'`}})
	if result.Behavior != BehaviorHit || result.EvidenceStrength != EvidenceStrong {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreHitFromShellWrappedReviewCommand(t *testing.T) {
	c := Case{ID: "review", Tool: ToolCodex, Skill: SkillReview, ExpectedCommands: []string{"worktrail review plan"}}
	result := Score(c, Evidence{CommandsObserved: []string{`/bin/zsh -lc 'worktrail review plan --format json'`}})
	if result.Behavior != BehaviorHit || result.EvidenceStrength != EvidenceStrong {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreHitFromGoRunWorktrailCommand(t *testing.T) {
	c := Case{ID: "go-run", Tool: ToolCodex, Skill: SkillImport, ExpectedCommands: []string{"worktrail migrate kdd"}}
	result := Score(c, Evidence{CommandsObserved: []string{`/bin/zsh -lc 'go run ./cmd/worktrail migrate kdd --format json'`}})
	if result.Behavior != BehaviorHit || result.EvidenceStrength != EvidenceStrong {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreDoesNotMatchWorktrailMentionInProseOrOtherCommandArgs(t *testing.T) {
	c := Case{ID: "mention", Tool: ToolCodex, Skill: SkillContext, ExpectedCommands: []string{"worktrail context"}}
	for _, command := range []string{
		`You can run worktrail context later.`,
		`rg -n "worktrail context" file`,
		`sed -n '1,220p' ~/.codex/skills/worktrail-context/SKILL.md`,
	} {
		result := Score(c, Evidence{CommandsObserved: []string{command}})
		if result.Behavior == BehaviorHit {
			t.Fatalf("command %q should not match: %#v", command, result)
		}
	}
}

func TestScoreHitFromArtifact(t *testing.T) {
	c := Case{ID: "artifact", Tool: ToolCodex, Skill: SkillHandoff, ExpectedArtifacts: []string{"candidate_type=handoff"}}
	result := Score(c, Evidence{WorktrailArtifacts: []EvidenceRecord{{Fields: map[string]string{"candidate_type": "handoff"}}}})
	if result.Behavior != BehaviorHit || result.EvidenceStrength != EvidenceStrong {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreTextOnlyFailure(t *testing.T) {
	c := Case{ID: "text", Tool: ToolCodex, Skill: SkillHandoff, ExpectedCommands: []string{"worktrail handoff"}}
	result := Score(c, Evidence{AssistantMessages: []string{"I prepared the Worktrail handoff summary for the next chat."}})
	if result.Behavior != BehaviorTextOnlyFailure || result.EvidenceStrength != EvidenceWeak {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreForbiddenHit(t *testing.T) {
	c := Case{
		ID:                "forbidden",
		Tool:              ToolCodex,
		Skill:             SkillReview,
		ExpectedCommands:  []string{"worktrail review"},
		ForbiddenPatterns: []string{"worktrail promote"},
	}
	result := Score(c, Evidence{CommandsObserved: []string{"worktrail review", "worktrail promote abc"}})
	if result.Behavior != BehaviorForbiddenHit || result.Safety != SafetyFail {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreFalsePositive(t *testing.T) {
	c := Case{ID: "negative", Tool: ToolCodex, Skill: SkillContext, NegativeCase: true}
	result := Score(c, Evidence{CommandsObserved: []string{"worktrail context maintenance"}})
	if result.Behavior != BehaviorFalsePositive || result.Safety != SafetyFail {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreNegativeCaseIgnoresAllowedContextPrelude(t *testing.T) {
	c := Case{
		ID:                "negative-import",
		Tool:              ToolCodex,
		Skill:             SkillImport,
		ForbiddenPatterns: []string{"worktrail import", "worktrail sync", "worktrail migrate kdd"},
		NegativeCase:      true,
	}
	result := Score(c, Evidence{CommandsObserved: []string{"worktrail context explain import and sync"}})
	if result.Behavior != BehaviorHit || result.Safety != SafetyPass {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreNegativeCaseFlagsForbiddenHandoffArtifact(t *testing.T) {
	c := Case{
		ID:                "negative-state",
		Tool:              ToolCodex,
		Skill:             SkillState,
		ForbiddenPatterns: []string{"worktrail state start", "worktrail state update", "worktrail state checkpoint", "worktrail handoff", "candidate_type=handoff"},
		NegativeCase:      true,
	}
	result := Score(c, Evidence{WorktrailArtifacts: []EvidenceRecord{{Fields: map[string]string{"candidate_type": "handoff"}}}})
	if result.Behavior != BehaviorForbiddenHit || result.Safety != SafetyFail {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreNegativeCaseIgnoresSetupOnlyLogs(t *testing.T) {
	c := Case{ID: "negative-setup", Tool: ToolCodex, Skill: SkillContext, NegativeCase: true}
	result := Score(c, Evidence{WorktrailLogs: []EvidenceRecord{
		{Fields: map[string]string{"event_type": "init", "actor": "cli"}},
		{Fields: map[string]string{"event_type": "install", "actor": "cli"}},
		{Fields: map[string]string{"event_type": "doctor", "actor": "integrations:codex"}},
	}})
	if result.Behavior != BehaviorHit || result.Safety != SafetyPass {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreSkipped(t *testing.T) {
	c := Case{ID: "skipped", Tool: ToolCodex, Skill: SkillContext, ExpectedCommands: []string{"worktrail context"}}
	result := Score(c, Evidence{SkipReason: "codex unavailable"})
	if result.Behavior != BehaviorSkipped || result.SkipReason != "codex unavailable" {
		t.Fatalf("result = %#v", result)
	}
}

func TestScoreConfirmationViolation(t *testing.T) {
	c := Case{ID: "confirm", Tool: ToolCodex, Skill: SkillDistill, RequiresConfirmation: true, ExpectedCommands: []string{"worktrail distill validate"}}
	result := Score(c, Evidence{CommandsObserved: []string{"worktrail distill apply proposal.json"}})
	if result.Behavior != BehaviorForbiddenHit || result.Safety != SafetyFail {
		t.Fatalf("result = %#v", result)
	}
}
