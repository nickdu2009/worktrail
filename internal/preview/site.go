package preview

import (
	"crypto/sha1"
	"encoding/hex"
	"html/template"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/yuin/goldmark"
)

const (
	homePagePath           = "index.html"
	candidatesOverviewPath = "candidates/index.html"
)

type site struct {
	Title            string
	Source           Source
	Metadata         []metadataItem
	HomePath         string
	CandidatesPath   string
	DocumentCount    int
	PendingCount     int
	Sections         []siteSection
	CandidateBuckets []siteCandidateBucket
}

type siteSection struct {
	Key        string
	Title      string
	OutputPath string
	Documents  []siteDocument
}

type siteDocument struct {
	Title      string
	Path       string
	OutputPath string
	Summary    string
	Metadata   []metadataItem
	Content    template.HTML
}

type siteCandidateBucket struct {
	Key         string
	Title       string
	Description string
	Candidates  []siteCandidate
}

type siteCandidate struct {
	ID         string
	Title      string
	Path       string
	OutputPath string
	Summary    string
	Type       string
	Status     string
	Metadata   []metadataItem
	Content    template.HTML
}

type slugger struct {
	seen map[string]string
}

func newSlugger() *slugger {
	return &slugger{seen: map[string]string{}}
}

func (s *slugger) Document(pathValue string) string {
	base := slugBase(pathValue)
	if existing, ok := s.seen[base]; ok && existing != pathValue {
		return base + "-" + shortHash(pathValue)
	}
	s.seen[base] = pathValue
	return base
}

func buildSite(md goldmark.Markdown, src Source, title string) (site, error) {
	if title == "" {
		title = "Untitled Worktrail Preview"
	}
	result := site{
		Title:          title,
		Source:         src,
		Metadata:       sortedMetadata(src.Metadata),
		HomePath:       homePagePath,
		CandidatesPath: candidatesOverviewPath,
		DocumentCount:  len(src.Children),
		PendingCount:   len(src.PendingCandidates),
	}

	sectionsByKey := map[string]int{}
	docSlugs := newSlugger()
	for _, child := range src.Children {
		rendered, err := renderMarkdown(md, child.Body)
		if err != nil {
			return site{}, err
		}
		key, sectionTitle := sectionForPath(child.Path)
		sectionIndex, ok := sectionsByKey[key]
		if !ok {
			result.Sections = append(result.Sections, siteSection{
				Key:        key,
				Title:      sectionTitle,
				OutputPath: path.Join("sections", safeSlugSegment(key)+".html"),
			})
			sectionIndex = len(result.Sections) - 1
			sectionsByKey[key] = sectionIndex
		}
		result.Sections[sectionIndex].Documents = append(result.Sections[sectionIndex].Documents, siteDocument{
			Title:      child.Title,
			Path:       child.Path,
			OutputPath: path.Join("docs", docSlugs.Document(child.Path)+".html"),
			Summary:    summaryFromMarkdown(child.Body, ""),
			Metadata:   sortedMetadata(child.Metadata),
			Content:    rendered,
		})
	}
	sort.SliceStable(result.Sections, func(i, j int) bool {
		left, right := result.Sections[i], result.Sections[j]
		if sectionOrder(left.Title) != sectionOrder(right.Title) {
			return sectionOrder(left.Title) < sectionOrder(right.Title)
		}
		return left.Title < right.Title
	})
	for i := range result.Sections {
		sort.SliceStable(result.Sections[i].Documents, func(a, b int) bool {
			return result.Sections[i].Documents[a].Path < result.Sections[i].Documents[b].Path
		})
	}

	bucketsByKey := map[string]int{}
	for _, pending := range src.PendingCandidates {
		rendered, err := renderMarkdown(md, pending.Body)
		if err != nil {
			return site{}, err
		}
		key, bucketTitle, description := classifyCandidateBucket(pending)
		bucketIndex, ok := bucketsByKey[key]
		if !ok {
			result.CandidateBuckets = append(result.CandidateBuckets, siteCandidateBucket{
				Key:         key,
				Title:       bucketTitle,
				Description: description,
			})
			bucketIndex = len(result.CandidateBuckets) - 1
			bucketsByKey[key] = bucketIndex
		}
		result.CandidateBuckets[bucketIndex].Candidates = append(result.CandidateBuckets[bucketIndex].Candidates, siteCandidate{
			ID:         pending.ID,
			Title:      pending.Title,
			Path:       pending.Path,
			OutputPath: path.Join("candidates", safeSlugSegment(pending.ID)+".html"),
			Summary:    summaryFromMarkdown(pending.Body, pending.Summary),
			Type:       pending.Metadata["candidate_type"],
			Status:     pending.Metadata["status"],
			Metadata:   sortedMetadata(pending.Metadata),
			Content:    rendered,
		})
	}
	sort.SliceStable(result.CandidateBuckets, func(i, j int) bool {
		left, right := result.CandidateBuckets[i], result.CandidateBuckets[j]
		if candidateBucketOrder(left.Key) != candidateBucketOrder(right.Key) {
			return candidateBucketOrder(left.Key) < candidateBucketOrder(right.Key)
		}
		return left.Title < right.Title
	})
	for i := range result.CandidateBuckets {
		sort.SliceStable(result.CandidateBuckets[i].Candidates, func(a, b int) bool {
			left := result.CandidateBuckets[i].Candidates[a]
			right := result.CandidateBuckets[i].Candidates[b]
			if left.Title != right.Title {
				return left.Title < right.Title
			}
			return left.ID < right.ID
		})
	}

	return result, nil
}

func classifyCandidateBucket(src Source) (string, string, string) {
	switch src.Metadata["candidate_surface"] {
	case model.CandidateSurfaceEvidence:
		return "evidence", "Evidence Candidates", "Transcript evidence and migration sources that still need distillation or evidence lifecycle decisions."
	case model.CandidateSurfaceSemantic:
		return "semantic", "Semantic Candidates", "Pending knowledge candidates that are already shaped like formal Worktrail knowledge."
	default:
		return "operational", "Operational Candidates", "Pending coordination or runtime candidates that are visible here but hidden from the default review/context inbox."
	}
}

func candidateBucketOrder(key string) int {
	switch key {
	case "semantic":
		return 0
	case "evidence":
		return 10
	case "operational":
		return 20
	default:
		return 1000
	}
}

var (
	markdownImageRE = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	markdownLinkRE  = regexp.MustCompile(`\[(.*?)\]\([^)]+\)`)
	htmlTagRE       = regexp.MustCompile(`<[^>]+>`)
)

func summaryFromMarkdown(body, fallback string) string {
	if text := cleanSummary(fallback); text != "" {
		return text
	}
	if text := firstReadableParagraph(body); text != "" {
		return text
	}
	return "No summary available."
}

func firstReadableParagraph(body string) string {
	var paragraph []string
	inFence := false
	fenceMarker := ""
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if fence, ok := markdownFenceMarker(line); ok {
			if !inFence {
				inFence = true
				fenceMarker = fence
			} else if fence == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			continue
		}
		if inFence {
			continue
		}
		if line == "" {
			if summary := cleanSummary(strings.Join(paragraph, " ")); summary != "" {
				return summary
			}
			paragraph = nil
			continue
		}
		if shouldSkipSummaryLine(line) {
			continue
		}
		if normalized := normalizeSummaryLine(line); normalized != "" {
			paragraph = append(paragraph, normalized)
		}
	}
	return cleanSummary(strings.Join(paragraph, " "))
}

func markdownFenceMarker(line string) (string, bool) {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "```"):
		return "```", true
	case strings.HasPrefix(line, "~~~"):
		return "~~~", true
	default:
		return "", false
	}
}

func shouldSkipSummaryLine(line string) bool {
	switch {
	case strings.HasPrefix(line, "#"):
		return true
	case isPureHTMLLine(line):
		return true
	case isMarkdownTableDivider(line):
		return true
	default:
		return false
	}
}

func normalizeSummaryLine(line string) string {
	line = trimSummaryPrefix(line)
	line = markdownImageRE.ReplaceAllString(line, "$1")
	line = markdownLinkRE.ReplaceAllString(line, "$1")
	line = htmlTagRE.ReplaceAllString(line, " ")
	line = strings.NewReplacer("**", "", "__", "", "`", "", "~~", "").Replace(line)
	return strings.Join(strings.Fields(line), " ")
}

func trimSummaryPrefix(line string) string {
	for {
		switch {
		case strings.HasPrefix(line, ">"):
			line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		case strings.HasPrefix(line, "- "), strings.HasPrefix(line, "* "), strings.HasPrefix(line, "+ "):
			line = strings.TrimSpace(line[2:])
		case hasTaskMarker(line):
			line = strings.TrimSpace(line[3:])
		default:
			prefixLen := orderedListPrefixLength(line)
			if prefixLen == 0 {
				return line
			}
			line = strings.TrimSpace(line[prefixLen:])
		}
	}
}

func hasTaskMarker(line string) bool {
	if len(line) < 4 || line[0] != '[' || line[2] != ']' {
		return false
	}
	return (line[1] == ' ' || line[1] == 'x' || line[1] == 'X') && line[3] == ' '
}

func orderedListPrefixLength(line string) int {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(line) {
		return 0
	}
	if (line[i] == '.' || line[i] == ')') && line[i+1] == ' ' {
		return i + 2
	}
	return 0
}

func isPureHTMLLine(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "<") && strings.HasSuffix(line, ">")
}

func isMarkdownTableDivider(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || !strings.Contains(line, "-") {
		return false
	}
	for _, r := range line {
		switch r {
		case '|', '-', ':', ' ':
		default:
			return false
		}
	}
	return true
}

func cleanSummary(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "`*_ ")
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}
	if len(value) > 180 {
		return strings.TrimSpace(value[:177]) + "..."
	}
	return value
}

func slugBase(pathValue string) string {
	trimmed := strings.TrimSuffix(pathValue, path.Ext(pathValue))
	if trimmed == "" {
		trimmed = pathValue
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(trimmed) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "document"
	}
	return out
}

func safeSlugSegment(value string) string {
	base := slugBase(value)
	if base == "" {
		return "item"
	}
	return base
}

func shortHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:4])
}
