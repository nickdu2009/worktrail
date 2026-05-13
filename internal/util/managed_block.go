package util

import "strings"

const (
	ManagedBegin = "<!-- BEGIN WORKTRAIL MANAGED BLOCK -->"
	ManagedEnd   = "<!-- END WORKTRAIL MANAGED BLOCK -->"
)

func ApplyManagedBlock(existing, body string) string {
	block := ManagedBegin + "\n" + strings.TrimSpace(body) + "\n" + ManagedEnd
	start := strings.Index(existing, ManagedBegin)
	end := strings.Index(existing, ManagedEnd)
	if start >= 0 && end >= start {
		end += len(ManagedEnd)
		next := strings.TrimSpace(existing[:start] + block + existing[end:])
		return next + "\n"
	}
	if strings.TrimSpace(existing) == "" {
		return block + "\n"
	}
	return strings.TrimRight(existing, "\n") + "\n\n" + block + "\n"
}

func RemoveManagedBlock(existing string) string {
	start := strings.Index(existing, ManagedBegin)
	end := strings.Index(existing, ManagedEnd)
	if start < 0 || end < start {
		return existing
	}
	end += len(ManagedEnd)
	return strings.TrimSpace(existing[:start]+existing[end:]) + "\n"
}
