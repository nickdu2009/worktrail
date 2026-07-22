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

// Version identifies the structural chunking policy exposed on Chunk records
// and hashed into chunk/group IDs.
const Version = "chunker-v2"

// SmallChunkThreshold is the architecture small-chunk floor used by MinPayload.
const SmallChunkThreshold = 80

// Chunk kinds emitted by the chunker.
const (
	KindText              = "text"
	KindTableRowGroup     = "table_row_group"
	KindTableCellFragment = "table_cell_fragment"
)

// Structural group kinds used in group identity material.
const (
	GroupKindText  = "text"
	GroupKindTable = "table"
)

// Document is the stable source and metadata supplied to the chunker.
type Document struct {
	Scope          string
	ID             string
	Path           string
	Title          string
	Type           string
	Topic          string
	Tags           []string
	Body           string
	SourceBaseByte int
	SourceSizeByte int
}

// Budget limits embedding inputs, including their metadata prefix.
type Budget struct {
	Target     int
	HardMax    int
	Overlap    int
	MinPayload int
}

// ChunkingPolicy is the versioned structural chunking configuration.
type ChunkingPolicy struct {
	Version    string
	Budget     Budget
	ConfigHash string
}

// ByteRange is a zero-based, half-open UTF-8 byte range in the original file.
type ByteRange struct {
	Start int
	End   int
}

// Chunk is a deterministic, source-citable embedding unit.
type Chunk struct {
	ChunkID           string
	Scope             string
	DocumentID        string
	Path              string
	Type              string
	Topic             string
	Tags              []string
	Order             int
	Kind              string
	StructuralGroupID string
	FragmentOrdinal   int
	HeadingBreadcrumb []string
	SourceStart       int
	SourceEnd         int
	ContextRange      *ByteRange
	GroupRange        *ByteRange
	Body              string
	MetadataTerms     string
	ContextTerms      string
	EmbeddingInput    string
	TokenCount        int
	EmbeddingHash     string
	PrevChunkID       string
	NextChunkID       string
	ChunkerVersion    string
	SourceSizeByte    int
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

// DefaultBudget returns the production structural-chunking budget frozen by the
// production-gate-assets budget-matrix (Target/HardMax/MinPayload; Overlap 64).
func DefaultBudget() Budget {
	return Budget{
		Target:     640,
		HardMax:    768,
		Overlap:    64,
		MinPayload: 80,
	}
}

// ConfigHash returns the canonical hash of a chunking budget.
func ConfigHash(budget Budget) string {
	return sha256Hex(fmt.Sprintf(
		"chunking_policy_v1\ntarget=%d\nhard_max=%d\noverlap=%d\nmin_payload=%d\n",
		budget.Target,
		budget.HardMax,
		budget.Overlap,
		budget.MinPayload,
	))
}

// DefaultPolicy returns the production chunking policy.
func DefaultPolicy() ChunkingPolicy {
	budget := DefaultBudget()
	return ChunkingPolicy{
		Version:    Version,
		Budget:     budget,
		ConfigHash: ConfigHash(budget),
	}
}

// PolicyWithBudget builds a policy for the current Version and budget.
func PolicyWithBudget(budget Budget) ChunkingPolicy {
	return ChunkingPolicy{
		Version:    Version,
		Budget:     budget,
		ConfigHash: ConfigHash(budget),
	}
}

// EvalPolicy builds an isolated eval chunker policy for one budget candidate.
// The version is always chunker-v2-eval-<config-hash> so candidates never share
// the production chunker-v2 profile or vector reuse boundary.
func EvalPolicy(budget Budget) ChunkingPolicy {
	hash := ConfigHash(budget)
	return ChunkingPolicy{
		Version:    "chunker-v2-eval-" + hash,
		Budget:     budget,
		ConfigHash: hash,
	}
}

// ChunkDocument creates deterministic structural chunks using DefaultPolicy.
func ChunkDocument(ctx context.Context, doc Document, counter contracts.TokenCounter) ([]Chunk, error) {
	return ChunkDocumentWithPolicy(ctx, doc, counter, DefaultPolicy())
}

// ChunkDocumentWithPolicy creates deterministic structural chunks with an
// explicit policy. Production callers should use ChunkDocument / DefaultPolicy.
func ChunkDocumentWithPolicy(ctx context.Context, doc Document, counter contracts.TokenCounter, policy ChunkingPolicy) ([]Chunk, error) {
	if err := policy.valid(); err != nil {
		return nil, err
	}
	return chunkDocument(ctx, doc, counter, policy.Budget)
}

func chunkDocument(ctx context.Context, doc Document, counter contracts.TokenCounter, budget Budget) ([]Chunk, error) {
	if counter == nil {
		return nil, fmt.Errorf("chunk: token counter is required")
	}
	if err := budget.valid(); err != nil {
		return nil, err
	}
	budget = budget.normalized()

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
		if next.table {
			if err := flush(); err != nil {
				return nil, err
			}
			if err := c.appendTable(ctx, next); err != nil {
				return nil, err
			}
			continue
		}

		if pending == nil {
			candidate, err := c.tokenCount(ctx, next.start, next.end, next.breadcrumb, "", string(c.source[next.start:next.end]))
			if err != nil {
				return nil, err
			}
			if candidate > budget.HardMax {
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
			candidate, err := c.tokenCount(ctx, next.start, next.end, next.breadcrumb, "", string(c.source[next.start:next.end]))
			if err != nil {
				return nil, err
			}
			if candidate > budget.HardMax {
				if err := c.appendForced(ctx, next); err != nil {
					return nil, err
				}
				continue
			}
			pendingCopy := next
			pending = &pendingCopy
			continue
		}

		candidate, err := c.tokenCount(ctx, pending.start, next.end, pending.breadcrumb, "", string(c.source[pending.start:next.end]))
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

		tokens, err := c.tokenCount(ctx, next.start, next.end, next.breadcrumb, "", string(c.source[next.start:next.end]))
		if err != nil {
			return nil, err
		}
		if tokens > budget.HardMax {
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
	linkSameGroupNeighbors(c.chunks)
	return c.chunks, nil
}

// Validate reports whether the policy fields are internally consistent.
func (p ChunkingPolicy) Validate() error {
	return p.valid()
}

func (p ChunkingPolicy) valid() error {
	if p.Version == "" {
		return fmt.Errorf("chunk: policy version is required")
	}
	if err := p.Budget.valid(); err != nil {
		return err
	}
	budget := p.Budget.normalized()
	floor := max(SmallChunkThreshold, budget.Overlap+1)
	if budget.MinPayload < floor {
		return fmt.Errorf("chunk: min payload must be at least %d", floor)
	}
	if budget.MinPayload >= budget.HardMax {
		return fmt.Errorf("chunk: min payload must be less than the hard maximum")
	}
	if p.ConfigHash == "" {
		return fmt.Errorf("chunk: policy config hash is required")
	}
	if want := ConfigHash(budget); p.ConfigHash != want {
		return fmt.Errorf("chunk: policy config hash mismatch")
	}
	return nil
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
	if b.MinPayload < 0 {
		return fmt.Errorf("chunk: min payload must not be negative")
	}
	return nil
}

func (b Budget) normalized() Budget {
	if b.MinPayload == 0 {
		floor := max(SmallChunkThreshold, b.Overlap+1)
		if floor < b.HardMax {
			b.MinPayload = floor
		} else {
			b.MinPayload = max(1, b.Overlap+1)
			if b.MinPayload >= b.HardMax {
				b.MinPayload = max(1, b.HardMax/2)
			}
		}
	}
	return b
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
	body := string(c.source[block.start:block.end])
	tokens, err := c.tokenCount(ctx, block.start, block.end, block.breadcrumb, "", body)
	if err != nil {
		return err
	}
	if tokens > c.budget.HardMax {
		return fmt.Errorf("chunk: internal oversized chunk at bytes [%d,%d)", block.start, block.end)
	}
	chunk, err := c.newTextChunk(block.start, block.end, block.start, block.end, block.breadcrumb, "", body, tokens, 0)
	if err != nil {
		return err
	}
	c.chunks = append(c.chunks, chunk)
	return nil
}

func (c *chunker) appendForced(ctx context.Context, block atom) error {
	start := block.start
	ordinal := 0
	for start < block.end {
		end, tokens, err := c.forcedEnd(ctx, start, block.end, block.breadcrumb, "")
		if err != nil {
			return err
		}
		if end <= start {
			return fmt.Errorf("chunk: unable to split oversized block at byte %d", start)
		}
		body := string(c.source[start:end])
		chunk, err := c.newTextChunk(start, end, block.start, block.end, block.breadcrumb, "", body, tokens, ordinal)
		if err != nil {
			return err
		}
		c.chunks = append(c.chunks, chunk)
		ordinal++
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

func (c *chunker) forcedEnd(ctx context.Context, start, limit int, breadcrumb []string, contextTerms string) (int, int, error) {
	end, tokens, err := forcedEndWithinBudget(c.source, start, limit, c.budget.HardMax, func(end int) (int, error) {
		return c.tokenCount(ctx, start, end, breadcrumb, contextTerms, string(c.source[start:end]))
	})
	if err != nil {
		return 0, 0, err
	}
	if end < 0 {
		return 0, 0, fmt.Errorf("chunk: metadata prefix alone exceeds hard maximum %d", c.budget.HardMax)
	}
	return end, tokens, nil
}

func (c *chunker) overlapStart(ctx context.Context, start, end int) (int, error) {
	if c.budget.Overlap == 0 {
		return end, nil
	}
	return overlapStartWithinBudget(c.source, start, end, c.budget.Overlap, func(candidate int) (int, error) {
		tokens, err := c.counter.CountTokens(ctx, string(c.source[candidate:end]))
		if err != nil {
			return 0, fmt.Errorf("chunk: count overlap tokens: %w", err)
		}
		return tokens, nil
	})
}

func forcedEndWithinBudget(source []byte, start, limit, hardMax int, count func(int) (int, error)) (int, int, error) {
	// The former linear scan stopped at the first over-budget prefix, so binary
	// search preserves the same monotonic-prefix assumption with fewer probes.
	ends := make([]int, 0)
	for end := nextRuneEnd(source, start); end <= limit; end = nextRuneEnd(source, end) {
		ends = append(ends, end)
		if end == limit {
			break
		}
	}

	low, high := 0, len(ends)
	lastSafe, lastTokens := -1, 0
	for low < high {
		middle := low + (high-low)/2
		tokens, err := count(ends[middle])
		if err != nil {
			return 0, 0, err
		}
		if tokens <= hardMax {
			lastSafe, lastTokens = middle, tokens
			low = middle + 1
		} else {
			high = middle
		}
	}
	if lastSafe < 0 {
		return -1, 0, nil
	}

	end := ends[lastSafe]
	preferred := end
	for preferred > start && !isSplitBoundary(source, preferred) {
		preferred = previousRuneStart(source, preferred)
	}
	if preferred <= start || preferred == end {
		return end, lastTokens, nil
	}
	tokens, err := count(preferred)
	return preferred, tokens, err
}

func overlapStartWithinBudget(source []byte, start, end, overlap int, count func(int) (int, error)) (int, error) {
	candidates := []int{end}
	for candidate := previousRuneStart(source, end); candidate > start; candidate = previousRuneStart(source, candidate) {
		if isStartBoundary(source, candidate) {
			candidates = append(candidates, candidate)
		}
	}

	if _, err := count(end); err != nil {
		return 0, err
	}
	low, high, best := 1, len(candidates), 0
	for low < high {
		middle := low + (high-low)/2
		tokens, err := count(candidates[middle])
		if err != nil {
			return 0, err
		}
		if tokens <= overlap {
			best = middle
			low = middle + 1
		} else {
			high = middle
		}
	}
	return candidates[best], nil
}

func (c *chunker) tokenCount(ctx context.Context, start, end int, breadcrumb []string, contextTerms, body string) (int, error) {
	input := embeddingInput(c.doc, breadcrumb, contextTerms, body)
	tokens, err := c.counter.CountTokens(ctx, input)
	if err != nil {
		return 0, fmt.Errorf("chunk: count tokens: %w", err)
	}
	if tokens < 0 {
		return 0, fmt.Errorf("chunk: token counter returned negative count %d", tokens)
	}
	return tokens, nil
}

func (c *chunker) countInput(ctx context.Context, breadcrumb []string, contextTerms, body string) (int, error) {
	input := embeddingInput(c.doc, breadcrumb, contextTerms, body)
	tokens, err := c.counter.CountTokens(ctx, input)
	if err != nil {
		return 0, fmt.Errorf("chunk: count tokens: %w", err)
	}
	if tokens < 0 {
		return 0, fmt.Errorf("chunk: token counter returned negative count %d", tokens)
	}
	return tokens, nil
}

func (c *chunker) newTextChunk(primaryStart, primaryEnd, groupStart, groupEnd int, breadcrumb []string, contextTerms, body string, tokens, ordinal int) (Chunk, error) {
	return c.newChunk(Chunk{
		Kind:              KindText,
		StructuralGroupID: c.groupID(GroupKindText, groupStart, groupEnd),
		FragmentOrdinal:   ordinal,
		HeadingBreadcrumb: append([]string(nil), breadcrumb...),
		SourceStart:       c.abs(primaryStart),
		SourceEnd:         c.abs(primaryEnd),
		GroupRange:        &ByteRange{Start: c.abs(groupStart), End: c.abs(groupEnd)},
		Body:              body,
		MetadataTerms:     metadataTerms(c.doc),
		ContextTerms:      contextTermsOrSection(contextTerms, breadcrumb),
		EmbeddingInput:    embeddingInput(c.doc, breadcrumb, contextTerms, body),
		TokenCount:        tokens,
	})
}

func (c *chunker) newChunk(base Chunk) (Chunk, error) {
	if err := c.validateAbsRange(base.SourceStart, base.SourceEnd); err != nil {
		return Chunk{}, err
	}
	if base.ContextRange != nil {
		if err := c.validateAbsRange(base.ContextRange.Start, base.ContextRange.End); err != nil {
			return Chunk{}, err
		}
	}
	if base.GroupRange != nil {
		if err := c.validateAbsRange(base.GroupRange.Start, base.GroupRange.End); err != nil {
			return Chunk{}, err
		}
		if base.SourceStart < base.GroupRange.Start || base.SourceEnd > base.GroupRange.End {
			return Chunk{}, fmt.Errorf(
				"chunk: primary range [%d,%d) escapes structural group [%d,%d)",
				base.SourceStart,
				base.SourceEnd,
				base.GroupRange.Start,
				base.GroupRange.End,
			)
		}
		if base.ContextRange != nil {
			if base.ContextRange.Start < base.GroupRange.Start || base.ContextRange.End > base.GroupRange.End {
				return Chunk{}, fmt.Errorf(
					"chunk: context range [%d,%d) escapes structural group [%d,%d)",
					base.ContextRange.Start,
					base.ContextRange.End,
					base.GroupRange.Start,
					base.GroupRange.End,
				)
			}
		}
	}
	base.DocumentID = c.doc.ID
	base.Scope = c.doc.Scope
	base.Path = c.doc.Path
	base.Type = c.doc.Type
	base.Topic = c.doc.Topic
	base.Tags = append([]string(nil), c.doc.Tags...)
	base.EmbeddingHash = sha256Hex(base.EmbeddingInput)
	base.ChunkerVersion = Version
	base.SourceSizeByte = c.sourceLimit()
	return base, nil
}

func (c *chunker) abs(local int) int {
	return c.doc.SourceBaseByte + local
}

func (c *chunker) sourceLimit() int {
	if c.doc.SourceSizeByte > 0 {
		return c.doc.SourceSizeByte
	}
	return c.doc.SourceBaseByte + len(c.source)
}

func (c *chunker) validateAbsRange(start, end int) error {
	limit := c.sourceLimit()
	if start < 0 || start >= end || end > limit {
		return fmt.Errorf("chunk: invalid absolute range [%d,%d) for source size %d", start, end, limit)
	}
	return nil
}

func (c *chunker) groupID(groupKind string, localStart, localEnd int) string {
	return structuralGroupID(c.doc, groupKind, c.abs(localStart), c.abs(localEnd))
}

func structuralGroupID(doc Document, groupKind string, absStart, absEnd int) string {
	return sha256Hex(strings.Join([]string{
		Version,
		doc.Scope,
		doc.ID,
		doc.Path,
		groupKind,
		fmt.Sprintf("%d", absStart),
		fmt.Sprintf("%d", absEnd),
	}, "\x1e"))
}

func embeddingInput(doc Document, breadcrumb []string, contextTerms, body string) string {
	var builder strings.Builder
	builder.WriteString(metadataTerms(doc))
	builder.WriteString("\nsection: ")
	builder.WriteString(metadataValue(strings.Join(breadcrumb, " > ")))
	builder.WriteString("\n\n")
	if contextTerms != "" {
		builder.WriteString(contextTerms)
		if !strings.HasSuffix(contextTerms, "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString(body)
	return builder.String()
}

func metadataTerms(doc Document) string {
	tags := append([]string(nil), doc.Tags...)
	sort.Strings(tags)
	return fmt.Sprintf(
		"path: %s\nscope: %s\ntitle: %s\ntype: %s\ntopic: %s\ntags: %s",
		metadataValue(doc.Path),
		metadataValue(doc.Scope),
		metadataValue(doc.Title),
		metadataValue(doc.Type),
		metadataValue(doc.Topic),
		metadataValue(strings.Join(tags, ", ")),
	)
}

func contextTermsOrSection(contextTerms string, breadcrumb []string) string {
	if contextTerms != "" {
		return contextTerms
	}
	return metadataValue(strings.Join(breadcrumb, " > "))
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
		chunk.Kind,
		strings.Join(chunk.HeadingBreadcrumb, "\x1f"),
		fmt.Sprintf("%d", chunk.Order),
		fmt.Sprintf("%d", chunk.SourceStart),
		fmt.Sprintf("%d", chunk.SourceEnd),
		fmt.Sprintf("%d", chunk.FragmentOrdinal),
		chunk.EmbeddingHash,
	}, "\x1e"))
}

func linkSameGroupNeighbors(chunks []Chunk) {
	for i := range chunks {
		chunks[i].PrevChunkID = ""
		chunks[i].NextChunkID = ""
	}
	for i := 1; i < len(chunks); i++ {
		if chunks[i].StructuralGroupID == "" || chunks[i].StructuralGroupID != chunks[i-1].StructuralGroupID {
			continue
		}
		chunks[i].PrevChunkID = chunks[i-1].ChunkID
		chunks[i-1].NextChunkID = chunks[i].ChunkID
	}
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
