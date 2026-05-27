package model

import "strings"

const (
	CandidateSurfaceSemantic    = "semantic"
	CandidateSurfaceEvidence    = "evidence"
	CandidateSurfaceOperational = "operational"
)

func CandidateSurface(candidateType string) string {
	switch {
	case IsEvidenceCandidateType(candidateType):
		return CandidateSurfaceEvidence
	case IsSemanticCandidateType(candidateType):
		return CandidateSurfaceSemantic
	default:
		return CandidateSurfaceOperational
	}
}

func IsEvidenceCandidateType(candidateType string) bool {
	candidateType = strings.TrimSpace(candidateType)
	return candidateType == CandidateTypeTranscriptNotes || candidateType == CandidateTypeMigrationSource
}

func PendingInboxVisible(status, candidateType string, includeEvidence bool) bool {
	if strings.TrimSpace(status) != "pending" {
		return false
	}
	switch CandidateSurface(candidateType) {
	case CandidateSurfaceEvidence:
		return includeEvidence
	case CandidateSurfaceSemantic:
		return true
	default:
		return false
	}
}
