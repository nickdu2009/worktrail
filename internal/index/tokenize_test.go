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
