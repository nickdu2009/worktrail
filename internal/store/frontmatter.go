package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

const Marker = "---worktrail"

type Document struct {
	Meta          map[string]any
	Body          string
	BodyStartByte int
}

func ParseMarkdown(data []byte) (Document, error) {
	if !hasMarkerPrefix(data) {
		return Document{}, errors.New("missing worktrail frontmatter")
	}
	markerEnd := len(Marker)
	lineEnd, ok := consumeLineEnding(data, markerEnd)
	if !ok {
		return Document{}, errors.New("missing worktrail frontmatter")
	}

	terminatorRel := indexTerminator(data[lineEnd:])
	if terminatorRel < 0 {
		return Document{}, errors.New("missing frontmatter terminator")
	}
	rawMeta := data[lineEnd : lineEnd+terminatorRel]
	afterTerminator := lineEnd + terminatorRel + len("---")
	bodyStart, ok := consumeLineEnding(data, afterTerminator)
	if !ok {
		return Document{}, errors.New("missing frontmatter terminator")
	}
	for bodyStart < len(data) {
		next, ok := consumeLineEnding(data, bodyStart)
		if !ok {
			break
		}
		bodyStart = next
	}

	meta := map[string]any{}
	dec := json.NewDecoder(bytes.NewReader(rawMeta))
	dec.UseNumber()
	if err := dec.Decode(&meta); err != nil {
		return Document{}, err
	}
	return Document{
		Meta:          meta,
		Body:          string(data[bodyStart:]),
		BodyStartByte: bodyStart,
	}, nil
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

func hasMarkerPrefix(data []byte) bool {
	if len(data) < len(Marker) {
		return false
	}
	if !bytes.Equal(data[:len(Marker)], []byte(Marker)) {
		return false
	}
	_, ok := consumeLineEnding(data, len(Marker))
	return ok
}

func consumeLineEnding(data []byte, offset int) (int, bool) {
	if offset >= len(data) {
		return 0, false
	}
	switch data[offset] {
	case '\n':
		return offset + 1, true
	case '\r':
		if offset+1 < len(data) && data[offset+1] == '\n' {
			return offset + 2, true
		}
		return offset + 1, true
	default:
		return 0, false
	}
}

func indexTerminator(data []byte) int {
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\n':
			if hasTerminatorAt(data, i+1) {
				return i + 1
			}
		case '\r':
			next := i + 1
			if next < len(data) && data[next] == '\n' {
				next++
			}
			if hasTerminatorAt(data, next) {
				return next
			}
		}
	}
	return -1
}

func hasTerminatorAt(data []byte, offset int) bool {
	if offset+3 > len(data) {
		return false
	}
	return data[offset] == '-' && data[offset+1] == '-' && data[offset+2] == '-'
}
