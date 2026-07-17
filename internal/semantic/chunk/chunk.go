// Package chunk splits Markdown documents into deterministic, source-citable
// embedding units.
package chunk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Version identifies the structural chunking policy.
const Version = "chunker-v1"

// Document is the stable source and metadata supplied to the chunker.
type Document struct {
	Scope string
	ID    string
	Path  string
	Title string
	Type  string
	Topic string
	Tags  []string
	Body  string
}

// Budget limits embedding inputs, including their metadata prefix.
type Budget struct {
	Target  int
	HardMax int
	Overlap int
}

// DefaultBudget returns an independent initial structural-chunking budget.
func DefaultBudget() Budget {
	return Budget{
		Target:  512,
		HardMax: 768,
		Overlap: 64,
	}
}

// Chunk is a deterministic, source-citable embedding unit.
type Chunk struct {
	ChunkID           string
	Scope             string
	DocumentID        string
	Path              string
	Order             int
	HeadingBreadcrumb []string
	SourceStart       int
	SourceEnd         int
	Body              string
	EmbeddingInput    string
	TokenCount        int
	EmbeddingHash     string
	PrevChunkID       string
	NextChunkID       string
	ChunkerVersion    string
}

type atom struct {
	start      int
	end        int
	breadcrumb []string
	table      bool
}

type chunker struct {
	doc     Document
	source  []byte
	counter contracts.TokenCounter
	budget  Budget
	chunks  []Chunk
}

// ChunkDocument creates deterministic structural chunks using only the
// injected token counter.
func ChunkDocument(ctx context.Context, doc Document, counter contracts.TokenCounter) ([]Chunk, error) {
	return chunkDocument(ctx, doc, counter, DefaultBudget())
}

func chunkDocument(ctx context.Context, doc Document, counter contracts.TokenCounter, budget Budget) ([]Chunk, error) {
	if counter == nil {
		return nil, fmt.Errorf("chunk: token counter is required")
	}
	if err := budget.valid(); err != nil {
		return nil, err
	}

	c := chunker{
		doc:     doc,
		source:  []byte(doc.Body),
		counter: counter,
		budget:  budget,
	}
	atoms, err := collectAtoms(c.source)
	if err != nil {
		return nil, err
	}

	var pending *atom
	flush := func() error {
		if pending == nil {
			return nil
		}
		if err := c.appendChunk(ctx, *pending); err != nil {
			return err
		}
		pending = nil
		return nil
	}

	for _, next := range atoms {
		if pending == nil {
			candidate, err := c.tokenCount(ctx, next.start, next.end, next.breadcrumb)
			if err != nil {
				return nil, err
			}
			if candidate > budget.HardMax {
				if next.table {
					return nil, tableTooLargeError(next, budget)
				}
				if err := c.appendForced(ctx, next); err != nil {
					return nil, err
				}
				continue
			}
			pendingCopy := next
			pending = &pendingCopy
			continue
		}

		if !sameBreadcrumb(pending.breadcrumb, next.breadcrumb) {
			if err := flush(); err != nil {
				return nil, err
			}
			candidate, err := c.tokenCount(ctx, next.start, next.end, next.breadcrumb)
			if err != nil {
				return nil, err
			}
			if candidate > budget.HardMax {
				if next.table {
					return nil, tableTooLargeError(next, budget)
				}
				if err := c.appendForced(ctx, next); err != nil {
					return nil, err
				}
				continue
			}
			pendingCopy := next
			pending = &pendingCopy
			continue
		}

		candidate, err := c.tokenCount(ctx, pending.start, next.end, pending.breadcrumb)
		if err != nil {
			return nil, err
		}
		if candidate <= budget.Target {
			pending.end = next.end
			continue
		}
		if err := flush(); err != nil {
			return nil, err
		}

		tokens, err := c.tokenCount(ctx, next.start, next.end, next.breadcrumb)
		if err != nil {
			return nil, err
		}
		if tokens > budget.HardMax {
			if next.table {
				return nil, tableTooLargeError(next, budget)
			}
			if err := c.appendForced(ctx, next); err != nil {
				return nil, err
			}
			continue
		}
		pendingCopy := next
		pending = &pendingCopy
	}
	if err := flush(); err != nil {
		return nil, err
	}

	for i := range c.chunks {
		c.chunks[i].Order = i
		c.chunks[i].ChunkID = chunkID(doc, c.chunks[i])
	}
	for i := range c.chunks {
		if i > 0 {
			c.chunks[i].PrevChunkID = c.chunks[i-1].ChunkID
		}
		if i+1 < len(c.chunks) {
			c.chunks[i].NextChunkID = c.chunks[i+1].ChunkID
		}
	}
	return c.chunks, nil
}

func (b Budget) valid() error {
	if b.Target <= 0 {
		return fmt.Errorf("chunk: target budget must be positive")
	}
	if b.HardMax < b.Target {
		return fmt.Errorf("chunk: hard maximum must be at least the target budget")
	}
	if b.Overlap < 0 {
		return fmt.Errorf("chunk: overlap must not be negative")
	}
	if b.Overlap > b.HardMax {
		return fmt.Errorf("chunk: overlap must not exceed the hard maximum")
	}
	return nil
}

func collectAtoms(source []byte) ([]atom, error) {
	if len(source) == 0 {
		return nil, nil
	}
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	root := md.Parser().Parse(text.NewReader(source))
	breadcrumbs := make([]string, 0, 6)
	var atoms []atom

	for node := root.FirstChild(); node != nil; node = node.NextSibling() {
		if heading, ok := node.(*gast.Heading); ok {
			level := heading.Level
			if level > len(breadcrumbs)+1 {
				level = len(breadcrumbs) + 1
			}
			breadcrumbs = append(breadcrumbs[:level-1], headingText(heading, source))
			continue
		}

		if list, ok := node.(*gast.List); ok {
			items, ok := listAtoms(list, source, breadcrumbs)
			if !ok {
				return nil, fmt.Errorf("chunk: cannot determine source offsets for list")
			}
			atoms = append(atoms, items...)
			continue
		}

		start, end, ok := sourceRange(node, source)
		if !ok {
			continue
		}
		if fenced, ok := node.(*gast.FencedCodeBlock); ok {
			start, end, ok = fencedRange(fenced, source, start, end)
			if !ok {
				return nil, fmt.Errorf("chunk: cannot determine source offsets for fenced code block")
			}
		}
		atoms = append(atoms, atom{
			start:      start,
			end:        end,
			breadcrumb: append([]string(nil), breadcrumbs...),
			table:      isTable(node),
		})
	}
	return atoms, nil
}

func headingText(heading *gast.Heading, source []byte) string {
	return strings.TrimSpace(string(heading.Text(source)))
}

func listAtoms(list *gast.List, source []byte, breadcrumbs []string) ([]atom, bool) {
	listStart, listEnd, ok := sourceRange(list, source)
	if !ok {
		return nil, false
	}
	var starts []int
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		start, _, itemOK := sourceRange(item, source)
		if !itemOK {
			return nil, false
		}
		starts = append(starts, start)
	}
	if len(starts) == 0 {
		return nil, false
	}
	if starts[0] < listStart {
		return nil, false
	}

	items := make([]atom, 0, len(starts))
	for i, start := range starts {
		end := listEnd
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		items = append(items, atom{
			start:      start,
			end:        end,
			breadcrumb: append([]string(nil), breadcrumbs...),
		})
	}
	return items, true
}

func sourceRange(node gast.Node, source []byte) (int, int, bool) {
	start, end := len(source), -1
	_ = gast.Walk(node, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		if pos := n.Pos(); pos >= 0 {
			start = min(start, pos)
		}
		if n.Type() == gast.TypeBlock {
			for i := 0; i < n.Lines().Len(); i++ {
				segment := n.Lines().At(i)
				start = min(start, segment.Start)
				end = max(end, segment.Stop)
			}
		}
		if value, ok := n.(*gast.Text); ok {
			start = min(start, value.Segment.Start)
			end = max(end, value.Segment.Stop)
		}
		return gast.WalkContinue, nil
	})
	if start == len(source) || end < start {
		return 0, 0, false
	}
	start = lineStart(source, start)
	end = rangeEnd(source, end)
	if start >= end {
		return 0, 0, false
	}
	return start, end, true
}

func fencedRange(_ *gast.FencedCodeBlock, source []byte, start, end int) (int, int, bool) {
	for line := lineStart(source, start); ; {
		fence, width, ok := openingFence(source[line:lineEnd(source, line)])
		if ok {
			for next := lineEnd(source, line); next < len(source); next = lineEnd(source, next) {
				if closingFence(source[next:lineEnd(source, next)], fence, width) {
					return line, lineEnd(source, next), true
				}
			}
			return line, len(source), true
		}
		if line == 0 {
			break
		}
		line = previousLineStart(source, line)
	}
	return start, end, true
}

func openingFence(line []byte) (byte, int, bool) {
	line = trimIndent(line)
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return 0, 0, false
	}
	width := 0
	for width < len(line) && line[width] == line[0] {
		width++
	}
	return line[0], width, width >= 3
}

func closingFence(line []byte, fence byte, width int) bool {
	line = trimIndent(line)
	count := 0
	for count < len(line) && line[count] == fence {
		count++
	}
	return count >= width && strings.TrimSpace(string(line[count:])) == ""
}

func trimIndent(line []byte) []byte {
	for i := 0; i < len(line) && i < 3; i++ {
		if line[i] != ' ' {
			return line[i:]
		}
	}
	return line
}

func isTable(node gast.Node) bool {
	_, ok := node.(*extast.Table)
	return ok
}

func (c *chunker) appendChunk(ctx context.Context, block atom) error {
	tokens, err := c.tokenCount(ctx, block.start, block.end, block.breadcrumb)
	if err != nil {
		return err
	}
	if tokens > c.budget.HardMax {
		return fmt.Errorf("chunk: internal oversized chunk at bytes [%d,%d)", block.start, block.end)
	}
	c.chunks = append(c.chunks, c.newChunk(block.start, block.end, block.breadcrumb, tokens))
	return nil
}

func (c *chunker) appendForced(ctx context.Context, block atom) error {
	start := block.start
	for start < block.end {
		end, tokens, err := c.forcedEnd(ctx, start, block.end, block.breadcrumb)
		if err != nil {
			return err
		}
		if end <= start {
			return fmt.Errorf("chunk: unable to split oversized block at byte %d", start)
		}
		c.chunks = append(c.chunks, c.newChunk(start, end, block.breadcrumb, tokens))
		if end == block.end {
			return nil
		}
		next, err := c.overlapStart(ctx, start, end)
		if err != nil {
			return err
		}
		if next <= start || next >= end {
			start = end
		} else {
			start = next
		}
	}
	return nil
}

func (c *chunker) forcedEnd(ctx context.Context, start, limit int, breadcrumb []string) (int, int, error) {
	lastSafe, lastPreferred, lastTokens := -1, -1, 0
	for end := nextRuneEnd(c.source, start); end <= limit; end = nextRuneEnd(c.source, end) {
		tokens, err := c.tokenCount(ctx, start, end, breadcrumb)
		if err != nil {
			return 0, 0, err
		}
		if tokens > c.budget.HardMax {
			break
		}
		lastSafe, lastTokens = end, tokens
		if isSplitBoundary(c.source, end) {
			lastPreferred = end
		}
		if end == limit {
			break
		}
	}
	if lastSafe < 0 {
		return 0, 0, fmt.Errorf("chunk: metadata prefix alone exceeds hard maximum %d", c.budget.HardMax)
	}
	if lastPreferred > start {
		tokens, err := c.tokenCount(ctx, start, lastPreferred, breadcrumb)
		if err != nil {
			return 0, 0, err
		}
		return lastPreferred, tokens, nil
	}
	return lastSafe, lastTokens, nil
}

func (c *chunker) overlapStart(ctx context.Context, start, end int) (int, error) {
	if c.budget.Overlap == 0 {
		return end, nil
	}
	best := end
	for candidate := end; candidate > start; candidate = previousRuneStart(c.source, candidate) {
		if candidate != end && !isStartBoundary(c.source, candidate) {
			continue
		}
		tokens, err := c.counter.CountTokens(ctx, string(c.source[candidate:end]))
		if err != nil {
			return 0, fmt.Errorf("chunk: count overlap tokens: %w", err)
		}
		if tokens > c.budget.Overlap {
			break
		}
		best = candidate
	}
	return best, nil
}

func (c *chunker) tokenCount(ctx context.Context, start, end int, breadcrumb []string) (int, error) {
	input := embeddingInput(c.doc, breadcrumb, string(c.source[start:end]))
	tokens, err := c.counter.CountTokens(ctx, input)
	if err != nil {
		return 0, fmt.Errorf("chunk: count tokens: %w", err)
	}
	if tokens < 0 {
		return 0, fmt.Errorf("chunk: token counter returned negative count %d", tokens)
	}
	return tokens, nil
}

func (c *chunker) newChunk(start, end int, breadcrumb []string, tokens int) Chunk {
	body := string(c.source[start:end])
	input := embeddingInput(c.doc, breadcrumb, body)
	return Chunk{
		DocumentID:        c.doc.ID,
		Scope:             c.doc.Scope,
		Path:              c.doc.Path,
		HeadingBreadcrumb: append([]string(nil), breadcrumb...),
		SourceStart:       start,
		SourceEnd:         end,
		Body:              body,
		EmbeddingInput:    input,
		TokenCount:        tokens,
		EmbeddingHash:     sha256Hex(input),
		ChunkerVersion:    Version,
	}
}

func embeddingInput(doc Document, breadcrumb []string, body string) string {
	tags := append([]string(nil), doc.Tags...)
	sort.Strings(tags)
	return fmt.Sprintf(
		"path: %s\nscope: %s\ntitle: %s\ntype: %s\ntopic: %s\ntags: %s\nsection: %s\n\n%s",
		metadataValue(doc.Path),
		metadataValue(doc.Scope),
		metadataValue(doc.Title),
		metadataValue(doc.Type),
		metadataValue(doc.Topic),
		metadataValue(strings.Join(tags, ", ")),
		metadataValue(strings.Join(breadcrumb, " > ")),
		body,
	)
}

func metadataValue(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}

func chunkID(doc Document, chunk Chunk) string {
	return sha256Hex(strings.Join([]string{
		Version,
		doc.Scope,
		doc.ID,
		doc.Path,
		strings.Join(chunk.HeadingBreadcrumb, "\x1f"),
		fmt.Sprintf("%d", chunk.Order),
		fmt.Sprintf("%d", chunk.SourceStart),
		fmt.Sprintf("%d", chunk.SourceEnd),
		chunk.Body,
	}, "\x1e"))
}

func tableTooLargeError(block atom, budget Budget) error {
	return fmt.Errorf(
		"chunk: table at bytes [%d,%d) exceeds hard maximum %d; table row splitting is not supported",
		block.start,
		block.end,
		budget.HardMax,
	)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sameBreadcrumb(left, right []string) bool {
	return strings.Join(left, "\x00") == strings.Join(right, "\x00")
}

func lineStart(source []byte, position int) int {
	position = min(position, len(source))
	for position > 0 && source[position-1] != '\n' {
		position--
	}
	return position
}

func lineEnd(source []byte, position int) int {
	position = min(position, len(source))
	for position < len(source) && source[position] != '\n' {
		position++
	}
	if position < len(source) {
		position++
	}
	return position
}

func rangeEnd(source []byte, position int) int {
	position = min(position, len(source))
	if position > 0 && source[position-1] == '\n' {
		return position
	}
	return lineEnd(source, position)
}

func previousLineStart(source []byte, position int) int {
	if position == 0 {
		return 0
	}
	return lineStart(source, position-1)
}

func nextRuneEnd(source []byte, start int) int {
	_, size := utf8.DecodeRune(source[start:])
	if size == 0 {
		return start
	}
	return start + size
}

func previousRuneStart(source []byte, end int) int {
	_, size := utf8.DecodeLastRune(source[:end])
	if size == 0 {
		return end
	}
	return end - size
}

func isSplitBoundary(source []byte, end int) bool {
	if end == len(source) {
		return true
	}
	r, _ := utf8.DecodeLastRune(source[:end])
	return unicode.IsSpace(r)
}

func isStartBoundary(source []byte, start int) bool {
	if start == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRune(source[:start])
	return unicode.IsSpace(r)
}
