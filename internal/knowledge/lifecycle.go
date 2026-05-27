package knowledge

import (
	"fmt"
	"strings"
)

const (
	LifecycleCurrent    = "current"
	LifecycleHistorical = "historical"
	LifecycleRetired    = "retired"
)

func IsValidLifecycle(lifecycle string) bool {
	switch strings.ToLower(strings.TrimSpace(lifecycle)) {
	case "", LifecycleCurrent, LifecycleHistorical, LifecycleRetired:
		return true
	default:
		return false
	}
}

func NormalizeLifecycle(lifecycle, stage, status string) string {
	switch strings.ToLower(strings.TrimSpace(lifecycle)) {
	case LifecycleHistorical:
		return LifecycleHistorical
	case LifecycleRetired:
		return LifecycleRetired
	case LifecycleCurrent:
		return LifecycleCurrent
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case LifecycleHistorical:
		return LifecycleHistorical
	case LifecycleRetired:
		return LifecycleRetired
	}
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case LifecycleHistorical:
		return LifecycleHistorical
	case LifecycleRetired:
		return LifecycleRetired
	default:
		return LifecycleCurrent
	}
}

func IsNonCurrentLifecycle(lifecycle string) bool {
	switch strings.ToLower(strings.TrimSpace(lifecycle)) {
	case LifecycleHistorical, LifecycleRetired:
		return true
	default:
		return false
	}
}

func ParseLifecycleList(raw string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		value := strings.ToLower(strings.TrimSpace(part))
		switch value {
		case "":
			continue
		case LifecycleHistorical:
			out = appendLifecycle(out, LifecycleHistorical)
		case LifecycleRetired:
			out = appendLifecycle(out, LifecycleRetired)
		case LifecycleCurrent:
			out = appendLifecycle(out, LifecycleCurrent)
		default:
			return nil, fmt.Errorf("invalid lifecycle %q (allowed: current, historical, retired)", strings.TrimSpace(part))
		}
	}
	return out, nil
}

func IncludesLifecycle(allowed []string, lifecycle string) bool {
	if len(allowed) == 0 {
		return lifecycle == LifecycleCurrent
	}
	for _, item := range allowed {
		if strings.EqualFold(strings.TrimSpace(item), lifecycle) {
			return true
		}
	}
	return false
}

func appendLifecycle(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
