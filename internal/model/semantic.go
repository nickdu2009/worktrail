package model

import "strings"

var semanticTargetPrefixes = map[string]string{
	"architecture": "architecture/",
	"decision":     "decisions/",
	"glossary":     "glossary/",
	"integration":  "integrations/",
	"lesson":       "lessons/",
	"prompt":       "prompts/",
	"requirement":  "requirements/",
	"rule":         "rules/",
	"validation":   "validation/",
	"workflow":     "workflows/",
}

func CanonicalSemanticCandidateType(typ string) string {
	typ = strings.TrimSpace(typ)
	if typ == "adr" {
		return "decision"
	}
	return typ
}

func IsSemanticCandidateType(typ string) bool {
	typ = CanonicalSemanticCandidateType(typ)
	if typ == "project" || typ == "index" {
		return true
	}
	_, ok := semanticTargetPrefixes[typ]
	return ok
}

func SemanticTargetPathMatches(typ, targetPath string) bool {
	typ = CanonicalSemanticCandidateType(typ)
	targetPath = strings.TrimSpace(targetPath)
	switch typ {
	case "project":
		return targetPath == "project.md"
	case "index":
		return targetPath == "index.md"
	}
	prefix, ok := semanticTargetPrefixes[typ]
	return ok && strings.HasPrefix(targetPath, prefix)
}
