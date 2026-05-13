package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

const Marker = "---worktrail"

type Document struct {
	Meta map[string]any
	Body string
}

func ParseMarkdown(data []byte) (Document, error) {
	text := string(data)
	if !strings.HasPrefix(text, Marker+"\n") {
		return Document{}, errors.New("missing worktrail frontmatter")
	}
	rest := text[len(Marker)+1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Document{}, errors.New("missing frontmatter terminator")
	}
	rawMeta := rest[:end]
	body := strings.TrimLeft(rest[end+4:], "\r\n")
	meta := map[string]any{}
	dec := json.NewDecoder(strings.NewReader(rawMeta))
	dec.UseNumber()
	if err := dec.Decode(&meta); err != nil {
		return Document{}, err
	}
	return Document{Meta: meta, Body: body}, nil
}

func RenderMarkdown(meta any, body string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(Marker)
	buf.WriteByte('\n')
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(meta); err != nil {
		return nil, err
	}
	buf.WriteString("---\n\n")
	buf.WriteString(strings.TrimSpace(body))
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}
