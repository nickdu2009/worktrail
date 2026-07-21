package chunk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// ErrIrreducibleTable is the sentinel for typed irreducible-table failures.
var ErrIrreducibleTable = errors.New("irreducible table")

// IrreducibleReason classifies why a table cannot be chunked under HardMax.
type IrreducibleReason string

const (
	IrreducibleReasonMetadataPrefix IrreducibleReason = "metadata_prefix"
	IrreducibleReasonHeaderContext  IrreducibleReason = "header_context"
	IrreducibleReasonColumnContext  IrreducibleReason = "column_context"
	IrreducibleReasonCellPayload    IrreducibleReason = "cell_payload"
)

// IrreducibleTableError is returned when table context leaves no MinPayload room
// or a cell cannot make forward progress under HardMax.
type IrreducibleTableError struct {
	Path       string
	BlockKind  string
	StartByte  int
	EndByte    int
	TokenCount int
	HardMax    int
	Reason     IrreducibleReason
}

func (e *IrreducibleTableError) Error() string {
	return fmt.Sprintf(
		"chunk: irreducible table %s at bytes [%d,%d) measured=%d hard_max=%d reason=%s",
		e.Path,
		e.StartByte,
		e.EndByte,
		e.TokenCount,
		e.HardMax,
		e.Reason,
	)
}

func (e *IrreducibleTableError) Is(target error) bool {
	return target == ErrIrreducibleTable
}

type tableColumn struct {
	name      string
	value     string
	cellStart int
	cellEnd   int
}

type tableRow struct {
	start   int
	end     int
	columns []tableColumn
}

type parsedTable struct {
	start           int
	end             int
	headerStart     int
	headerEnd       int
	headerContext   string
	columnNames     []string
	rows            []tableRow
	breadcrumb      []string
	structuralStart int
	structuralEnd   int
}

type rowGroup struct {
	start int
	end   int
	body  string
}

type columnGroup struct {
	start   int
	end     int
	names   []string
	body    string
	context string
}

func (c *chunker) appendTable(ctx context.Context, block atom) error {
	table, err := parseGFMTable(c.source, block)
	if err != nil {
		return err
	}

	fullBody := string(c.source[table.start:table.end])
	fullTokens, err := c.countInput(ctx, table.breadcrumb, "", fullBody)
	if err != nil {
		return err
	}
	if fullTokens <= c.budget.HardMax {
		chunk, err := c.newTableChunk(tableChunkSpec{
			kind:         KindTableRowGroup,
			primaryStart: table.start,
			primaryEnd:   table.end,
			contextStart: -1,
			contextEnd:   -1,
			groupStart:   table.structuralStart,
			groupEnd:     table.structuralEnd,
			breadcrumb:   table.breadcrumb,
			contextTerms: "",
			body:         fullBody,
			tokens:       fullTokens,
			fragmentOrd:  0,
		})
		if err != nil {
			return err
		}
		c.chunks = append(c.chunks, chunk)
		return nil
	}

	if err := c.ensurePayloadRoom(ctx, table, table.headerContext, IrreducibleReasonHeaderContext, table.headerStart, table.headerEnd); err != nil {
		return err
	}

	var current *rowGroup
	emitGroup := func(group rowGroup) error {
		tokens, err := c.countInput(ctx, table.breadcrumb, table.headerContext, group.body)
		if err != nil {
			return err
		}
		if tokens > c.budget.HardMax {
			return fmt.Errorf("chunk: internal oversized table row group at bytes [%d,%d)", group.start, group.end)
		}
		chunk, err := c.newTableChunk(tableChunkSpec{
			kind:         KindTableRowGroup,
			primaryStart: group.start,
			primaryEnd:   group.end,
			contextStart: table.headerStart,
			contextEnd:   table.headerEnd,
			groupStart:   table.structuralStart,
			groupEnd:     table.structuralEnd,
			breadcrumb:   table.breadcrumb,
			contextTerms: table.headerContext,
			body:         group.body,
			tokens:       tokens,
			fragmentOrd:  0,
		})
		if err != nil {
			return err
		}
		c.chunks = append(c.chunks, chunk)
		return nil
	}

	for _, row := range table.rows {
		rowBody := string(c.source[row.start:row.end])
		if current == nil {
			singleTokens, err := c.countInput(ctx, table.breadcrumb, table.headerContext, rowBody)
			if err != nil {
				return err
			}
			if singleTokens > c.budget.HardMax {
				if err := c.appendWideRow(ctx, table, row); err != nil {
					return err
				}
				continue
			}
			current = &rowGroup{start: row.start, end: row.end, body: rowBody}
			continue
		}

		candidateBody := current.body + rowBody
		candidateTokens, err := c.countInput(ctx, table.breadcrumb, table.headerContext, candidateBody)
		if err != nil {
			return err
		}
		if candidateTokens <= c.budget.Target {
			current.end = row.end
			current.body = candidateBody
			continue
		}

		if err := emitGroup(*current); err != nil {
			return err
		}
		current = nil

		singleTokens, err := c.countInput(ctx, table.breadcrumb, table.headerContext, rowBody)
		if err != nil {
			return err
		}
		if singleTokens > c.budget.HardMax {
			if err := c.appendWideRow(ctx, table, row); err != nil {
				return err
			}
			continue
		}
		current = &rowGroup{start: row.start, end: row.end, body: rowBody}
	}
	if current != nil {
		return emitGroup(*current)
	}
	return nil
}

func (c *chunker) appendWideRow(ctx context.Context, table parsedTable, row tableRow) error {
	if len(row.columns) == 0 {
		return c.irreducible(table, IrreducibleReasonCellPayload, row.start, row.end, 0)
	}

	var current *columnGroup
	emit := func(group columnGroup) error {
		tokens, err := c.countInput(ctx, table.breadcrumb, group.context, group.body)
		if err != nil {
			return err
		}
		if tokens > c.budget.HardMax {
			return c.appendCellFragments(ctx, table, row, group)
		}
		chunk, err := c.newTableChunk(tableChunkSpec{
			kind:         KindTableRowGroup,
			primaryStart: group.start,
			primaryEnd:   group.end,
			contextStart: -1,
			contextEnd:   -1,
			groupStart:   table.structuralStart,
			groupEnd:     table.structuralEnd,
			breadcrumb:   table.breadcrumb,
			contextTerms: group.context,
			body:         group.body,
			tokens:       tokens,
			fragmentOrd:  0,
		})
		if err != nil {
			return err
		}
		c.chunks = append(c.chunks, chunk)
		return nil
	}

	for _, column := range row.columns {
		if column.cellEnd <= column.cellStart {
			column.cellEnd = column.cellStart + 1
			if column.cellEnd > row.end {
				column.cellEnd = row.end
			}
			if column.cellEnd <= column.cellStart {
				return c.irreducible(table, IrreducibleReasonCellPayload, row.start, row.end, 0)
			}
		}
		labelContext := column.name
		if err := c.ensurePayloadRoom(ctx, table, labelContext, IrreducibleReasonColumnContext, column.cellStart, column.cellEnd); err != nil {
			return err
		}
		entry := formatColumnValue(column.name, column.value)
		if current == nil {
			current = &columnGroup{
				start:   column.cellStart,
				end:     column.cellEnd,
				names:   []string{column.name},
				body:    entry,
				context: labelContext,
			}
			singleTokens, err := c.countInput(ctx, table.breadcrumb, current.context, current.body)
			if err != nil {
				return err
			}
			if singleTokens > c.budget.HardMax {
				group := *current
				current = nil
				if err := c.appendCellFragments(ctx, table, row, group); err != nil {
					return err
				}
			}
			continue
		}

		candidateNames := append(append([]string(nil), current.names...), column.name)
		candidateContext := strings.Join(candidateNames, " | ")
		candidateBody := current.body + "\n" + entry
		candidateTokens, err := c.countInput(ctx, table.breadcrumb, candidateContext, candidateBody)
		if err != nil {
			return err
		}
		if candidateTokens <= c.budget.Target {
			current.end = column.cellEnd
			current.names = candidateNames
			current.context = candidateContext
			current.body = candidateBody
			continue
		}

		if err := emit(*current); err != nil {
			return err
		}
		current = &columnGroup{
			start:   column.cellStart,
			end:     column.cellEnd,
			names:   []string{column.name},
			body:    entry,
			context: labelContext,
		}
		singleTokens, err := c.countInput(ctx, table.breadcrumb, current.context, current.body)
		if err != nil {
			return err
		}
		if singleTokens > c.budget.HardMax {
			group := *current
			current = nil
			if err := c.appendCellFragments(ctx, table, row, group); err != nil {
				return err
			}
		}
	}
	if current != nil {
		return emit(*current)
	}
	return nil
}

func (c *chunker) appendCellFragments(ctx context.Context, table parsedTable, row tableRow, group columnGroup) error {
	if len(group.names) != 1 {
		for _, name := range group.names {
			var column *tableColumn
			for i := range row.columns {
				if row.columns[i].name == name {
					column = &row.columns[i]
					break
				}
			}
			if column == nil {
				continue
			}
			single := columnGroup{
				start:   column.cellStart,
				end:     column.cellEnd,
				names:   []string{column.name},
				body:    formatColumnValue(column.name, column.value),
				context: column.name,
			}
			if err := c.appendCellFragments(ctx, table, row, single); err != nil {
				return err
			}
		}
		return nil
	}

	columnName := group.names[0]
	var column tableColumn
	found := false
	for _, candidate := range row.columns {
		if candidate.name == columnName {
			column = candidate
			found = true
			break
		}
	}
	if !found {
		return c.irreducible(table, IrreducibleReasonCellPayload, group.start, group.end, 0)
	}
	if column.cellEnd <= column.cellStart {
		column.cellEnd = column.cellStart + 1
		if column.cellEnd > row.end {
			column.cellEnd = row.end
		}
	}
	if err := c.ensurePayloadRoom(ctx, table, column.name, IrreducibleReasonColumnContext, column.cellStart, column.cellEnd); err != nil {
		return err
	}

	value := []byte(column.value)
	if len(value) == 0 {
		body := formatColumnValue(column.name, "")
		tokens, err := c.countInput(ctx, table.breadcrumb, column.name, body)
		if err != nil {
			return err
		}
		if tokens > c.budget.HardMax {
			return c.irreducible(table, IrreducibleReasonCellPayload, column.cellStart, column.cellEnd, tokens)
		}
		chunk, err := c.newTableChunk(tableChunkSpec{
			kind:         KindTableCellFragment,
			primaryStart: column.cellStart,
			primaryEnd:   column.cellEnd,
			contextStart: -1,
			contextEnd:   -1,
			groupStart:   table.structuralStart,
			groupEnd:     table.structuralEnd,
			breadcrumb:   table.breadcrumb,
			contextTerms: column.name,
			body:         body,
			tokens:       tokens,
			fragmentOrd:  0,
		})
		if err != nil {
			return err
		}
		c.chunks = append(c.chunks, chunk)
		return nil
	}

	start := 0
	ordinal := 0
	for start < len(value) {
		end, tokens, err := c.forcedEndOnBytes(ctx, value, start, len(value), table.breadcrumb, column.name, column.name)
		if err != nil {
			if tokens+c.budget.MinPayload > c.budget.HardMax || tokens > c.budget.HardMax {
				return c.irreducible(table, IrreducibleReasonCellPayload, column.cellStart, column.cellEnd, tokens)
			}
			return err
		}
		if end <= start {
			return c.irreducible(table, IrreducibleReasonCellPayload, column.cellStart, column.cellEnd, tokens)
		}
		fragment := string(value[start:end])
		body := formatColumnValue(column.name, fragment)
		counted, err := c.countInput(ctx, table.breadcrumb, column.name, body)
		if err != nil {
			return err
		}
		chunk, err := c.newTableChunk(tableChunkSpec{
			kind:         KindTableCellFragment,
			primaryStart: column.cellStart,
			primaryEnd:   column.cellEnd,
			contextStart: -1,
			contextEnd:   -1,
			groupStart:   table.structuralStart,
			groupEnd:     table.structuralEnd,
			breadcrumb:   table.breadcrumb,
			contextTerms: column.name,
			body:         body,
			tokens:       counted,
			fragmentOrd:  ordinal,
		})
		if err != nil {
			return err
		}
		c.chunks = append(c.chunks, chunk)
		ordinal++
		if end == len(value) {
			return nil
		}
		next, err := c.overlapStartOnBytes(ctx, value, start, end)
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

func (c *chunker) forcedEndOnBytes(ctx context.Context, source []byte, start, limit int, breadcrumb []string, contextTerms, columnName string) (int, int, error) {
	lastSafe, lastPreferred, lastTokens := -1, -1, 0
	for end := nextRuneEnd(source, start); end <= limit; end = nextRuneEnd(source, end) {
		body := formatColumnValue(columnName, string(source[start:end]))
		tokens, err := c.countInput(ctx, breadcrumb, contextTerms, body)
		if err != nil {
			return 0, 0, err
		}
		if tokens > c.budget.HardMax {
			break
		}
		lastSafe, lastTokens = end, tokens
		if isSplitBoundary(source, end) {
			lastPreferred = end
		}
		if end == limit {
			break
		}
	}
	if lastSafe < 0 {
		body := formatColumnValue(columnName, "")
		tokens, err := c.countInput(ctx, breadcrumb, contextTerms, body)
		if err != nil {
			return 0, tokens, err
		}
		return 0, tokens, fmt.Errorf("chunk: cell fragment prefix exceeds hard maximum %d", c.budget.HardMax)
	}
	if lastPreferred > start {
		body := formatColumnValue(columnName, string(source[start:lastPreferred]))
		tokens, err := c.countInput(ctx, breadcrumb, contextTerms, body)
		if err != nil {
			return 0, 0, err
		}
		return lastPreferred, tokens, nil
	}
	return lastSafe, lastTokens, nil
}

func (c *chunker) overlapStartOnBytes(ctx context.Context, source []byte, start, end int) (int, error) {
	if c.budget.Overlap == 0 {
		return end, nil
	}
	best := end
	for candidate := end; candidate > start; candidate = previousRuneStart(source, candidate) {
		if candidate != end && !isStartBoundary(source, candidate) {
			continue
		}
		tokens, err := c.counter.CountTokens(ctx, string(source[candidate:end]))
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

func (c *chunker) ensurePayloadRoom(ctx context.Context, table parsedTable, contextTerms string, reason IrreducibleReason, localStart, localEnd int) error {
	tokens, err := c.countInput(ctx, table.breadcrumb, contextTerms, "")
	if err != nil {
		return err
	}
	if tokens+c.budget.MinPayload > c.budget.HardMax {
		return c.irreducible(table, reason, localStart, localEnd, tokens)
	}
	return nil
}

func (c *chunker) irreducible(table parsedTable, reason IrreducibleReason, localStart, localEnd, tokens int) error {
	return &IrreducibleTableError{
		Path:       c.doc.Path,
		BlockKind:  GroupKindTable,
		StartByte:  c.abs(localStart),
		EndByte:    c.abs(localEnd),
		TokenCount: tokens,
		HardMax:    c.budget.HardMax,
		Reason:     reason,
	}
}

type tableChunkSpec struct {
	kind         string
	primaryStart int
	primaryEnd   int
	contextStart int
	contextEnd   int
	groupStart   int
	groupEnd     int
	breadcrumb   []string
	contextTerms string
	body         string
	tokens       int
	fragmentOrd  int
}

func (c *chunker) newTableChunk(spec tableChunkSpec) (Chunk, error) {
	base := Chunk{
		Kind:              spec.kind,
		StructuralGroupID: c.groupID(GroupKindTable, spec.groupStart, spec.groupEnd),
		FragmentOrdinal:   spec.fragmentOrd,
		HeadingBreadcrumb: append([]string(nil), spec.breadcrumb...),
		SourceStart:       c.abs(spec.primaryStart),
		SourceEnd:         c.abs(spec.primaryEnd),
		GroupRange: &ByteRange{
			Start: c.abs(spec.groupStart),
			End:   c.abs(spec.groupEnd),
		},
		Body:           spec.body,
		MetadataTerms:  metadataTerms(c.doc),
		ContextTerms:   spec.contextTerms,
		EmbeddingInput: embeddingInput(c.doc, spec.breadcrumb, spec.contextTerms, spec.body),
		TokenCount:     spec.tokens,
	}
	if spec.contextStart >= 0 && spec.contextEnd > spec.contextStart {
		base.ContextRange = &ByteRange{
			Start: c.abs(spec.contextStart),
			End:   c.abs(spec.contextEnd),
		}
	}
	return c.newChunk(base)
}

func parseGFMTable(source []byte, block atom) (parsedTable, error) {
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	root := md.Parser().Parse(text.NewReader(source))

	var tableNode *extast.Table
	for node := root.FirstChild(); node != nil; node = node.NextSibling() {
		candidate, ok := node.(*extast.Table)
		if !ok {
			continue
		}
		start, end, ok := sourceRange(candidate, source)
		if !ok {
			continue
		}
		if start == block.start && end == block.end {
			tableNode = candidate
			break
		}
		if start >= block.start && end <= block.end && tableNode == nil {
			tableNode = candidate
		}
	}
	if tableNode == nil {
		return parsedTable{}, fmt.Errorf("chunk: gfm table node not found at bytes [%d,%d)", block.start, block.end)
	}

	table := parsedTable{
		start:           block.start,
		end:             block.end,
		structuralStart: block.start,
		structuralEnd:   block.end,
		breadcrumb:      append([]string(nil), block.breadcrumb...),
	}

	var dataRows []*extast.TableRow
	for child := tableNode.FirstChild(); child != nil; child = child.NextSibling() {
		switch node := child.(type) {
		case *extast.TableHeader:
			names := make([]string, 0, node.ChildCount())
			for cell := node.FirstChild(); cell != nil; cell = cell.NextSibling() {
				names = append(names, strings.TrimSpace(string(cell.Text(source))))
			}
			table.columnNames = names
		case *extast.TableRow:
			dataRows = append(dataRows, node)
		}
	}
	if len(table.columnNames) == 0 {
		return parsedTable{}, fmt.Errorf("chunk: table at bytes [%d,%d) has no header", block.start, block.end)
	}

	if len(dataRows) == 0 {
		table.headerStart = block.start
		table.headerEnd = block.end
		table.headerContext = string(source[table.headerStart:table.headerEnd])
		return table, nil
	}

	firstRowStart, _, ok := sourceRange(dataRows[0], source)
	if !ok {
		// Fall back to the first newline after the header line when Goldmark
		// omits row segments for truncated or adversarial fuzz input.
		firstRowStart = lineEnd(source, table.headerStart)
		if firstRowStart >= block.end {
			return parsedTable{}, fmt.Errorf("chunk: cannot determine first table row offsets")
		}
	}
	firstRowStart = lineStart(source, firstRowStart)
	table.headerStart = block.start
	table.headerEnd = firstRowStart
	if table.headerEnd <= table.headerStart {
		return parsedTable{}, fmt.Errorf("chunk: invalid table header range at bytes [%d,%d)", block.start, block.end)
	}
	table.headerContext = string(source[table.headerStart:table.headerEnd])

	for i, rowNode := range dataRows {
		rowStart, rowEnd, ok := sourceRange(rowNode, source)
		if !ok {
			rowStart = firstRowStart
			if i > 0 {
				rowStart = table.rows[i-1].end
			}
			rowEnd = block.end
		}
		rowStart = lineStart(source, rowStart)
		rowEnd = rangeEnd(source, rowEnd)
		if rowEnd > block.end {
			rowEnd = block.end
		}
		if rowEnd <= rowStart {
			continue
		}
		row := tableRow{start: rowStart, end: rowEnd}
		cellIndex := 0
		for cell := rowNode.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cellStart, cellEnd, cellOK := sourceRange(cell, source)
			if !cellOK {
				// Truncated or partially parsed GFM may omit cell segments; fall
				// back to the row range so fuzz/malformed input stays bounded.
				cellStart, cellEnd = rowStart, rowEnd
			}
			if cellEnd <= cellStart {
				cellEnd = cellStart + 1
				if cellEnd > rowEnd {
					cellEnd = rowEnd
				}
				if cellEnd <= cellStart {
					cellStart, cellEnd = rowStart, rowEnd
				}
			}
			name := fmt.Sprintf("column_%d", cellIndex+1)
			if cellIndex < len(table.columnNames) && table.columnNames[cellIndex] != "" {
				name = table.columnNames[cellIndex]
			}
			row.columns = append(row.columns, tableColumn{
				name:      name,
				value:     string(cell.Text(source)),
				cellStart: cellStart,
				cellEnd:   cellEnd,
			})
			cellIndex++
		}
		table.rows = append(table.rows, row)
	}

	for i := range table.rows {
		if i+1 < len(table.rows) {
			table.rows[i].end = table.rows[i+1].start
		} else {
			table.rows[i].end = table.end
		}
	}
	return table, nil
}

func formatColumnValue(name, value string) string {
	return name + "=" + value
}
