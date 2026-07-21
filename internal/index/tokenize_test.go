package index

import (
	"strings"
	"testing"
)

func TestTokenizeChineseAndIdentifier(t *testing.T) {
	tokenizer := DefaultTokenizer()
	doc := tokenizer.TokenizeDocument(DocumentForTokenize{
		Title:   "当前索引方案",
		Topic:   "source_of_truth",
		Tags:    []string{"worktrail"},
		Path:    "rules/source-of-truth.md",
		Content: "需要支持中文分词和 worktrail index rebuild 命令搜索",
	})
	if len(doc.TitleTerms) == 0 {
		t.Fatalf("expected chinese title terms, got %+v", doc.TitleTerms)
	}
	joined := strings.Join(doc.IdentTerms, " ")
	if !strings.Contains(joined, "source") || !strings.Contains(joined, "worktrail") {
		t.Fatalf("expected identifier terms, got %+v", doc.IdentTerms)
	}
	query := tokenizer.TokenizeQuery("索引 rebuild")
	if len(query.Terms) == 0 {
		t.Fatalf("expected query terms, got %+v", query)
	}
}

func TestNormalizeTextCollapsesWhitespace(t *testing.T) {
	got := normalizeText("  Worktrail   Index  ")
	if got != "worktrail index" {
		t.Fatalf("normalizeText() = %q", got)
	}
}

func TestNewTokenizerIsIsolatedFromDefault(t *testing.T) {
	defaultTok := DefaultTokenizer()
	isolated := NewTokenizer()
	other := NewTokenizer()
	if isolated == defaultTok || other == defaultTok || isolated == other {
		t.Fatal("NewTokenizer() must return independent instances")
	}
	before := defaultTok.TokenizeQuery("索引 rebuild")
	custom := "语义隔离专有词"
	if err := isolated.LoadProjectDictionary([]string{custom}); err != nil {
		t.Fatalf("LoadProjectDictionary() error = %v", err)
	}
	isolatedQuery := isolated.TokenizeQuery(custom + " 索引")
	if !strings.Contains(strings.Join(isolatedQuery.Terms, " "), custom) {
		t.Fatalf("isolated tokenizer missing project term: %+v", isolatedQuery.Terms)
	}
	after := defaultTok.TokenizeQuery("索引 rebuild")
	if strings.Join(before.Terms, " ") != strings.Join(after.Terms, " ") {
		t.Fatalf("DefaultTokenizer changed: before=%v after=%v", before.Terms, after.Terms)
	}
	defaultProject := defaultTok.TokenizeQuery(custom)
	if strings.Contains(strings.Join(defaultProject.Terms, " "), custom) {
		t.Fatalf("DefaultTokenizer unexpectedly gained isolation term: %+v", defaultProject.Terms)
	}
}
