package index

import (
	"strings"
	"sync"

	"github.com/go-ego/gse"
)

type gseTokenizer struct {
	seg gse.Segmenter
	mu  sync.Mutex
}

var defaultTokenizer Tokenizer = newGSETokenizer()

func newGSETokenizer() Tokenizer {
	t := &gseTokenizer{}
	_ = t.seg.LoadDict()
	_ = t.LoadBaseDictionary(baseDictionaryWords())
	return t
}

func DefaultTokenizer() Tokenizer {
	return defaultTokenizer
}

func (t *gseTokenizer) LoadBaseDictionary(words []string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		t.seg.AddToken(word, 100)
	}
	return nil
}

func (t *gseTokenizer) LoadProjectDictionary(words []string) error {
	return t.LoadBaseDictionary(words)
}

func (t *gseTokenizer) TokenizeDocument(doc DocumentForTokenize) TokenizedDocument {
	title := normalizeText(doc.Title)
	topic := normalizeText(doc.Topic)
	body := normalizeText(doc.Content)
	if len(body) > maxTokenBodyBytes {
		body = body[:maxTokenBodyBytes]
	}
	titleTerms := t.segment(title)
	bodyTerms := t.segment(body)
	topicTerms := t.segment(topic)
	var tagTerms []string
	for _, tag := range doc.Tags {
		tagTerms = append(tagTerms, t.segment(normalizeText(tag))...)
	}
	identTerms := extractIdentifierTerms(doc.Path, doc.Title, doc.Topic, doc.Type)
	identTerms = append(identTerms, extractIdentifierTerms(doc.Tags...)...)
	all := append([]string{}, titleTerms...)
	all = append(all, bodyTerms...)
	all = append(all, topicTerms...)
	all = append(all, tagTerms...)
	all = append(all, identTerms...)
	return TokenizedDocument{
		TitleTerms: titleTerms,
		BodyTerms:  bodyTerms,
		TopicTerms: topicTerms,
		TagTerms:   tagTerms,
		IdentTerms: identTerms,
		AllTerms:   all,
		Excerpt:    excerptContent(doc.Content),
	}
}

func (t *gseTokenizer) TokenizeQuery(query string) TokenizedQuery {
	normalized := normalizeText(query)
	terms := t.segment(normalized)
	terms = append(terms, extractIdentifierTerms(query)...)
	return TokenizedQuery{
		Terms:      terms,
		Raw:        query,
		Normalized: normalized,
	}
}

func (t *gseTokenizer) segment(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	segments := t.seg.Cut(text, true)
	seen := map[string]bool{}
	var out []string
	for _, seg := range segments {
		seg = strings.TrimSpace(strings.ToLower(seg))
		if seg == "" || len([]rune(seg)) == 1 && !isHan(seg) {
			continue
		}
		if seen[seg] {
			continue
		}
		seen[seg] = true
		out = append(out, seg)
	}
	return out
}

func isHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func tokenizeEntry(entry Entry, tokenizer Tokenizer) TokenizedDocument {
	if tokenizer == nil {
		tokenizer = defaultTokenizer
	}
	return tokenizer.TokenizeDocument(DocumentForTokenize{
		Path:    entry.Path,
		Title:   entry.Title,
		Topic:   entry.Topic,
		Tags:    entry.Tags,
		Type:    entry.Type,
		Content: entry.Content,
	})
}
