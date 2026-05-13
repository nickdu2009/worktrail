package util

import "strings"

const (
	ManagedBegin = "<!-- BEGIN WORKTRAIL MANAGED BLOCK -->"
	ManagedEnd   = "<!-- END WORKTRAIL MANAGED BLOCK -->"

	HashManagedBegin = "# BEGIN WORKTRAIL MANAGED BLOCK"
	HashManagedEnd   = "# END WORKTRAIL MANAGED BLOCK"
)

func ApplyManagedBlock(existing, body string) string {
	return ApplyManagedBlockWithMarkers(existing, body, ManagedBegin, ManagedEnd)
}

func ApplyHashManagedBlock(existing, body string) string {
	return ApplyManagedBlockWithMarkers(existing, body, HashManagedBegin, HashManagedEnd)
}

func ApplyManagedBlockWithMarkers(existing, body, begin, endMarker string) string {
	block := begin + "\n" + strings.TrimSpace(body) + "\n" + endMarker
	start := strings.Index(existing, begin)
	end := strings.Index(existing, endMarker)
	if start >= 0 && end >= start {
		end += len(endMarker)
		next := strings.TrimSpace(existing[:start] + block + existing[end:])
		return next + "\n"
	}
	if strings.TrimSpace(existing) == "" {
		return block + "\n"
	}
	return strings.TrimRight(existing, "\n") + "\n\n" + block + "\n"
}

func RemoveManagedBlock(existing string) string {
	return RemoveManagedBlockWithMarkers(existing, ManagedBegin, ManagedEnd)
}

func RemoveManagedBlockWithMarkers(existing, begin, endMarker string) string {
	start := strings.Index(existing, begin)
	end := strings.Index(existing, endMarker)
	if start < 0 || end < start {
		return existing
	}
	end += len(endMarker)
	return strings.TrimSpace(existing[:start]+existing[end:]) + "\n"
}
