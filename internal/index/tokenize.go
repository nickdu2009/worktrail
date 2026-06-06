package index

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	maxTokenBodyBytes = 64 * 1024
	maxExcerptRunes   = 700
)

var (
	identifierPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*(?:[_-][A-Za-z0-9]+)+`)
	camelCasePattern  = regexp.MustCompile(`[A-Za-z]+[a-z][A-Z][A-Za-z0-9]*`)
	pathTokenPattern  = regexp.MustCompile(`[a-zA-Z0-9][a-zA-Z0-9._/-]*[a-zA-Z0-9]`)
)

type DocumentForTokenize struct {
	Path    string
	Title   string
	Topic   string
	Tags    []string
	Type    string
	Content string
}

type TokenizedDocument struct {
	TitleTerms []string
	BodyTerms  []string
	TopicTerms []string
	TagTerms   []string
	IdentTerms []string
	AllTerms   []string
	Excerpt    string
}

type TokenizedQuery struct {
	Terms      []string
	Phrases    []string
	Raw        string
	Normalized string
}

type Tokenizer interface {
	TokenizeDocument(doc DocumentForTokenize) TokenizedDocument
	TokenizeQuery(query string) TokenizedQuery
	LoadBaseDictionary(words []string) error
	LoadProjectDictionary(words []string) error
}

func normalizeText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\u3000':
			b.WriteRune(' ')
		case unicode.Is(unicode.Han, r):
			b.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '_' || r == '-' || r == '/' || r == '.' || r == ':':
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func excerptContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	runes := []rune(content)
	if len(runes) <= maxExcerptRunes {
		return content
	}
	return strings.TrimSpace(string(runes[:maxExcerptRunes])) + "..."
}

func extractIdentifierTerms(parts ...string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(term string) {
		term = strings.TrimSpace(strings.ToLower(term))
		if term == "" || seen[term] {
			return
		}
		seen[term] = true
		out = append(out, term)
	}
	for _, part := range parts {
		part = normalizeText(part)
		if part == "" {
			continue
		}
		for _, match := range identifierPattern.FindAllString(part, -1) {
			add(match)
			add(strings.ReplaceAll(match, "_", " "))
			add(strings.ReplaceAll(match, "-", " "))
			for _, piece := range strings.FieldsFunc(match, func(r rune) bool {
				return r == '_' || r == '-' || r == '/'
			}) {
				add(piece)
			}
		}
		for _, match := range camelCasePattern.FindAllString(part, -1) {
			add(match)
		}
		for _, match := range pathTokenPattern.FindAllString(part, -1) {
			add(match)
			add(strings.TrimSuffix(filepathBase(match), filepathExt(match)))
		}
	}
	return out
}

func filepathBase(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func filepathExt(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i:]
	}
	return ""
}

func joinTerms(groups ...[]string) string {
	seen := map[string]bool{}
	var out []string
	for _, group := range groups {
		for _, term := range group {
			term = strings.TrimSpace(term)
			if term == "" || seen[term] {
				continue
			}
			seen[term] = true
			out = append(out, term)
		}
	}
	return strings.Join(out, " ")
}

func baseDictionaryWords() []string {
	return []string{
		"worktrail",
		"handoff",
		"context",
		"distill",
		"source_of_truth",
		"source of truth",
		"supersedes",
		"superseded_by",
		"index rebuild",
		"index refresh",
		"pending review",
		"transcript notes",
		"migration source",
	}
}

func deriveProjectDictionary(entries []Entry) []string {
	seen := map[string]bool{}
	var out []string
	add := func(word string) {
		word = strings.TrimSpace(strings.ToLower(word))
		if word == "" || len(word) < 2 || seen[word] {
			return
		}
		seen[word] = true
		out = append(out, word)
	}
	for _, entry := range entries {
		if entry.Topic != "" {
			add(entry.Topic)
		}
		for _, tag := range entry.Tags {
			add(tag)
		}
		add(filepathBase(entry.Path))
		add(strings.ReplaceAll(filepathBase(entry.Path), "-", " "))
		add(entry.Title)
	}
	if len(out) > 256 {
		out = out[:256]
	}
	return out
}
