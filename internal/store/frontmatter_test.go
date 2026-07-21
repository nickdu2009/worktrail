package store

import (
	"strings"
	"testing"
)

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
	if string(b[doc.BodyStartByte:]) != doc.Body {
		t.Fatalf("BodyStartByte %d does not slice to body", doc.BodyStartByte)
	}
}

func TestParseMarkdownBodyStartByteVariants(t *testing.T) {
	cases := []struct {
		name string
		data string
		body string
	}{
		{
			name: "lf with blank line",
			data: "---worktrail\n{\"id\":\"a\"}\n---\n\nhello\n",
			body: "hello\n",
		},
		{
			name: "crlf with blank line",
			data: "---worktrail\r\n{\"id\":\"a\"}\r\n---\r\n\r\nhello\r\n",
			body: "hello\r\n",
		},
		{
			name: "lf without blank line",
			data: "---worktrail\n{\"id\":\"a\"}\n---\nhello\n",
			body: "hello\n",
		},
		{
			name: "crlf without blank line",
			data: "---worktrail\r\n{\"id\":\"a\"}\r\n---\r\nhello\r\n",
			body: "hello\r\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseMarkdown([]byte(tc.data))
			if err != nil {
				t.Fatalf("ParseMarkdown() error = %v", err)
			}
			if doc.Body != tc.body {
				t.Fatalf("Body = %q, want %q", doc.Body, tc.body)
			}
			if got := tc.data[doc.BodyStartByte:]; got != tc.body {
				t.Fatalf("slice at BodyStartByte = %q, want %q", got, tc.body)
			}
		})
	}
}

func TestParseMarkdownRejectsMissingAndMalformed(t *testing.T) {
	if _, err := ParseMarkdown([]byte("# no frontmatter\n")); err == nil || !strings.Contains(err.Error(), "missing worktrail frontmatter") {
		t.Fatalf("missing frontmatter error = %v", err)
	}
	if _, err := ParseMarkdown([]byte("---worktrail\n{\"id\":1\n")); err == nil || !strings.Contains(err.Error(), "missing frontmatter terminator") {
		t.Fatalf("missing terminator error = %v", err)
	}
	if _, err := ParseMarkdown([]byte("---worktrail\n{bad-json}\n---\n\nbody\n")); err == nil {
		t.Fatal("malformed frontmatter error = nil")
	}
}
