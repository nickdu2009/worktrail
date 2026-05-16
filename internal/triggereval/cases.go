package triggereval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

func LoadCases(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, err
	}
	if err := ValidateCases(cases); err != nil {
		return nil, err
	}
	return cases, nil
}

func ValidateCases(cases []Case) error {
	if len(cases) == 0 {
		return errors.New("case corpus is empty")
	}
	validSkills := setOf(WorktrailSkills())
	seenIDs := map[string]bool{}
	positiveBySkill := map[string]int{}
	negativeBySkill := map[string]int{}
	hasConfirmation := false
	var problems []string

	for i, c := range cases {
		where := fmt.Sprintf("case[%d]", i)
		id := strings.TrimSpace(c.ID)
		if id == "" {
			problems = append(problems, where+": id is required")
		} else if seenIDs[id] {
			problems = append(problems, where+": duplicate id "+id)
		}
		seenIDs[id] = true

		if c.Tool != ToolCodex {
			problems = append(problems, label(c, where)+": tool must be codex")
		}
		if !validSkills[c.Skill] {
			problems = append(problems, label(c, where)+": unknown skill "+c.Skill)
		}
		if strings.TrimSpace(c.Prompt) == "" {
			problems = append(problems, label(c, where)+": prompt is required")
		}
		if strings.TrimSpace(c.ExpectedBehavior) == "" {
			problems = append(problems, label(c, where)+": expected_behavior is required")
		}

		if c.NegativeCase {
			negativeBySkill[c.Skill]++
			if len(c.ExpectedCommands) > 0 || len(c.ExpectedArtifacts) > 0 {
				problems = append(problems, label(c, where)+": negative case must not declare expected commands or artifacts")
			}
		} else {
			positiveBySkill[c.Skill]++
			if len(c.ExpectedCommands) == 0 && len(c.ExpectedArtifacts) == 0 {
				problems = append(problems, label(c, where)+": positive case requires expected command or artifact")
			}
		}

		if c.RequiresConfirmation {
			hasConfirmation = true
			for _, cmd := range c.ExpectedCommands {
				if isMutatingCommand(cmd) {
					problems = append(problems, label(c, where)+": confirmation case must not expect mutating command "+cmd)
				}
			}
		}
	}

	for _, skill := range WorktrailSkills() {
		if positiveBySkill[skill] < 2 {
			problems = append(problems, fmt.Sprintf("%s requires at least two positive cases", skill))
		}
		if negativeBySkill[skill] < 1 {
			problems = append(problems, fmt.Sprintf("%s requires at least one negative case", skill))
		}
	}
	if !hasConfirmation {
		problems = append(problems, "at least one case must require confirmation")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func setOf(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
}

func label(c Case, fallback string) string {
	if c.ID == "" {
		return fallback
	}
	return c.ID
}

func isMutatingCommand(cmd string) bool {
	for _, prefix := range mutatingCommandPrefixes {
		if commandObserved(prefix, []string{cmd}) {
			return true
		}
	}
	return false
}
