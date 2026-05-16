package triggereval

import (
	"strings"
	"testing"
	"time"
)

func TestBuildReportAggregatesMetrics(t *testing.T) {
	cases := []Case{
		{ID: "p-hit", Skill: SkillContext, Tool: ToolCodex},
		{ID: "p-text", Skill: SkillContext, Tool: ToolCodex},
		{ID: "p-skip", Skill: SkillState, Tool: ToolCodex},
		{ID: "n-clean", Skill: SkillHandoff, Tool: ToolCodex, NegativeCase: true},
		{ID: "n-fp", Skill: SkillHandoff, Tool: ToolCodex, NegativeCase: true},
	}
	results := []Result{
		{CaseID: "p-hit", ExpectedSkill: SkillContext, Behavior: BehaviorHit},
		{CaseID: "p-text", ExpectedSkill: SkillContext, Behavior: BehaviorTextOnlyFailure},
		{CaseID: "p-skip", ExpectedSkill: SkillState, Behavior: BehaviorSkipped, SkipReason: "codex unavailable"},
		{CaseID: "n-clean", ExpectedSkill: SkillHandoff, Behavior: BehaviorHit},
		{CaseID: "n-fp", ExpectedSkill: SkillHandoff, Behavior: BehaviorFalsePositive},
	}
	report := BuildReport(cases, results, nil, ReportOptions{Date: time.Unix(0, 0).UTC(), Tool: ToolCodex})
	if report.Metrics.CommandHitRate != 0.5 {
		t.Fatalf("CommandHitRate = %v", report.Metrics.CommandHitRate)
	}
	if report.Metrics.TextOnlyFailureRate != 0.5 {
		t.Fatalf("TextOnlyFailureRate = %v", report.Metrics.TextOnlyFailureRate)
	}
	if report.Metrics.FalsePositiveRate != 0.5 {
		t.Fatalf("FalsePositiveRate = %v", report.Metrics.FalsePositiveRate)
	}
	if report.Metrics.SkipRate != 0.2 {
		t.Fatalf("SkipRate = %v", report.Metrics.SkipRate)
	}
	if report.Metrics.PerSkillHitRate[SkillContext] != 0.5 {
		t.Fatalf("PerSkillHitRate[%s] = %v", SkillContext, report.Metrics.PerSkillHitRate[SkillContext])
	}
}

func TestRenderReport(t *testing.T) {
	report := BuildReport(
		[]Case{{ID: "c1", Skill: SkillContext, Tool: ToolCodex}},
		[]Result{{CaseID: "c1", ExpectedSkill: SkillContext, Behavior: BehaviorHit, EvidenceStrength: EvidenceStrong}},
		[]Evidence{{CaseID: "c1", CommandsObserved: []string{"worktrail context task"}}},
		ReportOptions{Date: time.Unix(0, 0).UTC(), Tool: ToolCodex},
	)
	data, err := RenderJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"command_hit_rate"`) {
		t.Fatalf("json missing metrics: %s", data)
	}
	md := RenderMarkdown(report)
	if !strings.Contains(md, "Worktrail Trigger Eval Report") || !strings.Contains(md, "c1: hit") {
		t.Fatalf("unexpected markdown: %s", md)
	}
}
