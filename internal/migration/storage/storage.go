package storage

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/util"
)

const (
	PlanSchema     = "worktrail.migration.storage.plan.v1"
	ManifestSchema = "worktrail.migration.storage.manifest.v1"
)

type Plan struct {
	Schema      string     `json:"schema"`
	GeneratedAt time.Time  `json:"generated_at"`
	Root        string     `json:"root"`
	Items       []PlanItem `json:"items"`
	Warnings    []string   `json:"warnings,omitempty"`
}

type PlanItem struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path,omitempty"`
	Action     string `json:"action"`
	Reason     string `json:"reason,omitempty"`
}

type ApplyReport struct {
	Schema    string     `json:"schema"`
	AppliedAt time.Time  `json:"applied_at"`
	Root      string     `json:"root"`
	Manifest  string     `json:"manifest"`
	Created   int        `json:"created"`
	Skipped   int        `json:"skipped"`
	Warnings  []string   `json:"warnings,omitempty"`
	Items     []PlanItem `json:"items"`
}

func PlanRoot(root string) (Plan, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	plan := Plan{
		Schema:      PlanSchema,
		GeneratedAt: time.Now().UTC(),
		Root:        root,
		Items:       []PlanItem{},
	}
	if root == "" {
		return plan, fmt.Errorf("storage migration root is required")
	}
	if _, err := os.Stat(root); err != nil {
		return plan, err
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		item := planItemForPath(root, path, rel)
		if item.Action == "" {
			return nil
		}
		plan.Items = append(plan.Items, item)
		if item.Action == "warn" && item.Reason != "" {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s: %s", item.SourcePath, item.Reason))
		}
		return nil
	})
	return plan, err
}

func ApplyRoot(root string, confirm bool) (ApplyReport, error) {
	report := ApplyReport{
		Schema:    ManifestSchema,
		AppliedAt: time.Now().UTC(),
		Root:      filepath.Clean(strings.TrimSpace(root)),
		Items:     []PlanItem{},
	}
	if !confirm {
		return report, fmt.Errorf("worktrail migrate storage-apply requires --confirm")
	}
	plan, err := PlanRoot(root)
	if err != nil {
		return report, err
	}
	report.Items = append(report.Items, plan.Items...)
	report.Warnings = append(report.Warnings, plan.Warnings...)
	for _, item := range plan.Items {
		switch item.Action {
		case "copy":
			src := filepath.Join(root, filepath.FromSlash(item.SourcePath))
			dst := filepath.Join(root, filepath.FromSlash(item.TargetPath))
			if _, err := os.Stat(dst); err == nil {
				report.Skipped++
				continue
			}
			data, err := os.ReadFile(src)
			if err != nil {
				return report, err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return report, err
			}
			if err := util.AtomicWrite(dst, data, 0o644); err != nil {
				return report, err
			}
			report.Created++
		case "warn", "keep":
			report.Skipped++
		}
	}
	manifestPath := filepath.Join(root, "derived", "exports", "storage-manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return report, err
	}
	manifestBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return report, err
	}
	if err := util.AtomicWrite(manifestPath, append(manifestBytes, '\n'), 0o644); err != nil {
		return report, err
	}
	report.Manifest = filepath.ToSlash(filepath.Join("derived", "exports", "storage-manifest.json"))
	return report, nil
}

func planItemForPath(root, absPath, rel string) PlanItem {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || rel == "config.json" {
		return PlanItem{}
	}
	if strings.HasPrefix(rel, ".cache/") {
		return PlanItem{}
	}
	if isAlreadyVNextPath(rel) {
		return PlanItem{SourcePath: rel, Action: "keep", Reason: "already_vnext"}
	}
	switch {
	case rel == "project.md":
		return PlanItem{SourcePath: rel, TargetPath: rel, Action: "keep", Reason: "canonical_entry"}
	case strings.HasPrefix(rel, "architecture/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("knowledge", rel)))
	case strings.HasPrefix(rel, "requirements/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("knowledge", rel)))
	case strings.HasPrefix(rel, "decisions/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("knowledge", rel)))
	case strings.HasPrefix(rel, "validation/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("knowledge", rel)))
	case strings.HasPrefix(rel, "workflows/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("knowledge", rel)))
	case strings.HasPrefix(rel, "integrations/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("knowledge", rel)))
	case strings.HasPrefix(rel, "glossary/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("knowledge", rel)))
	case strings.HasPrefix(rel, "rules/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("knowledge", rel)))
	case strings.HasPrefix(rel, "prompts/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("knowledge", rel)))
	case strings.HasPrefix(rel, "handoffs/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("knowledge", rel)))
	case strings.HasPrefix(rel, "candidates/"):
		target, reason := stagingTargetForCandidate(absPath, rel)
		if target == "" {
			return PlanItem{SourcePath: rel, Action: "warn", Reason: reason}
		}
		return copyItem(rel, target)
	case strings.HasPrefix(rel, "state/active/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("runtime", "sessions", strings.TrimPrefix(rel, "state/active/"))))
	case strings.HasPrefix(rel, "state/archived/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("runtime", "sessions", strings.TrimPrefix(rel, "state/archived/"))))
	case strings.HasPrefix(rel, "state/checkpoints/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("runtime", "checkpoints", strings.TrimPrefix(rel, "state/checkpoints/"))))
	case strings.HasPrefix(rel, "logs/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("runtime", rel)))
	case strings.HasPrefix(rel, "index/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("derived", rel)))
	case strings.HasPrefix(rel, "exports/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("derived", rel)))
	case strings.HasPrefix(rel, "raw/"):
		return copyItem(rel, filepath.ToSlash(filepath.Join("derived", "cache", rel)))
	case rel == "index.md":
		return copyItem(rel, rel)
	default:
		return PlanItem{SourcePath: rel, Action: "warn", Reason: "no_storage_mapping"}
	}
}

func stagingTargetForCandidate(absPath, rel string) (string, string) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err.Error()
	}
	doc, err := store.ParseMarkdown(data)
	if err != nil {
		return "", "candidate frontmatter parse failed"
	}
	meta, err := model.NormalizeObjectMeta(rel, doc.Meta)
	if err != nil {
		return "", err.Error()
	}
	base := filepath.Base(rel)
	switch {
	case meta.IsEvidence():
		return filepath.ToSlash(filepath.Join("staging", "evidence", base)), ""
	case meta.IsDraft() && meta.DraftKind == model.DraftKindOperational:
		return filepath.ToSlash(filepath.Join("staging", "operational", base)), ""
	default:
		return filepath.ToSlash(filepath.Join("staging", "drafts", base)), ""
	}
}

func copyItem(source, target string) PlanItem {
	if source == target {
		return PlanItem{SourcePath: source, TargetPath: target, Action: "keep", Reason: "already_canonical"}
	}
	return PlanItem{SourcePath: source, TargetPath: target, Action: "copy"}
}

func isAlreadyVNextPath(rel string) bool {
	return strings.HasPrefix(rel, "knowledge/") ||
		strings.HasPrefix(rel, "staging/") ||
		strings.HasPrefix(rel, "runtime/") ||
		strings.HasPrefix(rel, "derived/")
}
