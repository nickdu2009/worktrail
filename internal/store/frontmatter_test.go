package store

import "testing"

func TestRenderParseMarkdown(t *testing.T) {
	b, err := RenderMarkdown(map[string]any{"schema": "worktrail.state.v1", "id": "st_test"}, "# Body\n")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseMarkdown(b)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Meta["id"] != "st_test" {
		t.Fatalf("id = %v", doc.Meta["id"])
	}
	if doc.Body != "# Body\n" {
		t.Fatalf("body = %q", doc.Body)
	}
}
