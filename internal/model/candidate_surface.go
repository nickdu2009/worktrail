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

func ObjectSurface(meta ObjectMetaV2) string {
	switch meta.ObjectKind {
	case ObjectKindEvidence:
		return CandidateSurfaceEvidence
	case ObjectKindDraft:
		if meta.DraftKind == DraftKindOperational {
			return CandidateSurfaceOperational
		}
		return CandidateSurfaceSemantic
	case ObjectKindRuntime:
		return CandidateSurfaceOperational
	case ObjectKindKnowledgeDoc:
		return CandidateSurfaceSemantic
	default:
		return CandidateSurfaceOperational
	}
}

func ObjectPendingInboxVisible(meta ObjectMetaV2, includeEvidence bool) bool {
	switch ObjectSurface(meta) {
	case CandidateSurfaceEvidence:
		return meta.LifecycleStatus == LifecyclePendingDistill && includeEvidence
	case CandidateSurfaceSemantic:
		return meta.LifecycleStatus == LifecyclePendingReview
	default:
		return false
	}
}
