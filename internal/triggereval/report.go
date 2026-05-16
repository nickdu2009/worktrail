package triggereval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type ReportOptions struct {
	Date         time.Time
	Commit       string
	Tool         string
	RunnerConfig string
	KnownGaps    []string
}

func BuildReport(cases []Case, results []Result, evidences []Evidence, opts ReportOptions) Report {
	if opts.Date.IsZero() {
		opts.Date = time.Now().UTC()
	}
	if opts.Tool == "" {
		opts.Tool = ToolCodex
	}
	report := Report{
		Date:         opts.Date,
		Commit:       opts.Commit,
		Tool:         opts.Tool,
		RunnerConfig: opts.RunnerConfig,
		CaseCounts:   caseCounts(cases),
		ResultCounts: resultCounts(results),
		Metrics:      calculateMetrics(cases, results),
		Results:      append([]Result(nil), results...),
		Evidence:     evidenceSummaries(evidences),
		SkipReasons:  skipReasons(results),
		JudgeSummary: &JudgeSummary{Enabled: false},
		KnownGaps:    append([]string(nil), opts.KnownGaps...),
	}
	return report
}

func RenderJSON(report Report) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func RenderMarkdown(report Report) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "# Worktrail Trigger Eval Report\n\n")
	fmt.Fprintf(&buf, "- Date: %s\n", report.Date.UTC().Format(time.RFC3339))
	fmt.Fprintf(&buf, "- Tool: %s\n", report.Tool)
	if report.Commit != "" {
		fmt.Fprintf(&buf, "- Commit: %s\n", report.Commit)
	}
	if report.RunnerConfig != "" {
		fmt.Fprintf(&buf, "- Runner: %s\n", report.RunnerConfig)
	}
	fmt.Fprintf(&buf, "\n## Metrics\n\n")
	fmt.Fprintf(&buf, "- command_hit_rate: %.4f\n", report.Metrics.CommandHitRate)
	fmt.Fprintf(&buf, "- text_only_failure_rate: %.4f\n", report.Metrics.TextOnlyFailureRate)
	fmt.Fprintf(&buf, "- forbidden_hit_rate: %.4f\n", report.Metrics.ForbiddenHitRate)
	fmt.Fprintf(&buf, "- false_positive_rate: %.4f\n", report.Metrics.FalsePositiveRate)
	fmt.Fprintf(&buf, "- skip_rate: %.4f\n", report.Metrics.SkipRate)
	fmt.Fprintf(&buf, "\n## Results\n\n")
	for _, result := range report.Results {
		fmt.Fprintf(&buf, "- %s: %s (%s)", result.CaseID, result.Behavior, result.EvidenceStrength)
		if result.SkipReason != "" {
			fmt.Fprintf(&buf, " skip=%s", result.SkipReason)
		}
		buf.WriteByte('\n')
	}
	if len(report.SkipReasons) > 0 {
		fmt.Fprintf(&buf, "\n## Skip Reasons\n\n")
		for _, reason := range report.SkipReasons {
			fmt.Fprintf(&buf, "- %s\n", reason)
		}
	}
	if len(report.KnownGaps) > 0 {
		fmt.Fprintf(&buf, "\n## Known Gaps\n\n")
		for _, gap := range report.KnownGaps {
			fmt.Fprintf(&buf, "- %s\n", gap)
		}
	}
	return buf.String()
}

func calculateMetrics(cases []Case, results []Result) Metrics {
	byID := map[string]Result{}
	for _, result := range results {
		byID[result.CaseID] = result
	}
	metrics := Metrics{
		TotalCases:      len(cases),
		PerSkillHitRate: map[string]float64{},
	}
	type skillCounts struct{ hits, runnable int }
	perSkill := map[string]skillCounts{}
	var positiveHits, textOnly, forbidden, falsePositive int
	for _, c := range cases {
		result := byID[c.ID]
		if c.NegativeCase {
			metrics.NegativeCases++
		} else {
			metrics.PositiveCases++
		}
		if result.Behavior == BehaviorSkipped {
			metrics.SkippedCases++
			continue
		}
		if c.NegativeCase {
			metrics.RunnableNegativeCases++
			if result.Behavior == BehaviorFalsePositive {
				falsePositive++
			}
		} else {
			metrics.RunnablePositiveCases++
			counts := perSkill[c.Skill]
			counts.runnable++
			if result.Behavior == BehaviorHit {
				positiveHits++
				counts.hits++
			}
			perSkill[c.Skill] = counts
			if result.Behavior == BehaviorTextOnlyFailure {
				textOnly++
			}
		}
		if result.Behavior == BehaviorForbiddenHit {
			forbidden++
		}
	}
	metrics.CommandHitRate = rate(positiveHits, metrics.RunnablePositiveCases)
	metrics.TextOnlyFailureRate = rate(textOnly, metrics.RunnablePositiveCases)
	metrics.ForbiddenHitRate = rate(forbidden, metrics.TotalCases-metrics.SkippedCases)
	metrics.FalsePositiveRate = rate(falsePositive, metrics.RunnableNegativeCases)
	metrics.SkipRate = rate(metrics.SkippedCases, metrics.TotalCases)
	for _, skill := range WorktrailSkills() {
		counts := perSkill[skill]
		metrics.PerSkillHitRate[skill] = rate(counts.hits, counts.runnable)
	}
	return metrics
}

func caseCounts(cases []Case) map[string]int {
	out := map[string]int{"total": len(cases)}
	for _, c := range cases {
		if c.NegativeCase {
			out["negative"]++
		} else {
			out["positive"]++
		}
		out["skill:"+c.Skill]++
	}
	return out
}

func resultCounts(results []Result) map[string]int {
	out := map[string]int{}
	for _, result := range results {
		out[result.Behavior]++
	}
	return out
}

func evidenceSummaries(evidences []Evidence) []EvidenceSummary {
	out := make([]EvidenceSummary, 0, len(evidences))
	for _, evidence := range evidences {
		summary := EvidenceSummary{
			CaseID:           evidence.CaseID,
			CommandsObserved: append([]string(nil), evidence.CommandsObserved...),
			SkipReason:       evidence.SkipReason,
		}
		for _, rec := range evidence.WorktrailArtifacts {
			summary.Artifacts = append(summary.Artifacts, summarizeRecord(rec))
		}
		for _, rec := range evidence.WorktrailLogs {
			summary.Logs = append(summary.Logs, summarizeRecord(rec))
		}
		sort.Strings(summary.Artifacts)
		sort.Strings(summary.Logs)
		out = append(out, summary)
	}
	return out
}

func summarizeRecord(rec EvidenceRecord) string {
	var parts []string
	for key, value := range rec.Fields {
		parts = append(parts, key+"="+value)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func skipReasons(results []Result) []string {
	seen := map[string]bool{}
	var out []string
	for _, result := range results {
		if result.SkipReason == "" || seen[result.SkipReason] {
			continue
		}
		seen[result.SkipReason] = true
		out = append(out, result.SkipReason)
	}
	sort.Strings(out)
	return out
}

func rate(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}
