---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "worktrail-sqlite-gse-index-design",
  "scope": "project",
  "type": "architecture",
  "title": "Worktrail SQLite and GSE Index Design",
  "status": "active",
  "lifecycle": "current",
  "topic": "search-architecture"
}
---

# Worktrail SQLite + GSE Index Design

Last updated: 2026-07-18

Status: implemented (historical design for the lexical index subsystem)

## Relationship To Local Semantic Recall

This document is the implemented lexical baseline. Its no-daemon and
no-vector constraints describe **this lexical subsystem only**, not the
complete v1.0.0 product after the July 2026 semantic-recall scope revision.

The opt-in semantic subsystem defined in
`worktrail-local-semantic-recall-architecture.md` does not alter this entry
schema or default search contract. It uses separate profile-local generation
databases for chunks, chunk FTS, and sqlite-vec vectors; starts a managed
llama.app process only for explicit semantic work; and degrades back to this
lexical path when unavailable.

## Summary

This document records the implemented replacement of Worktrail's former JSON
index with a local SQLite-backed lexical index that supports Chinese search
through a Go-side token pipeline built on `gse`.

For this lexical subsystem, the design preserves:

- Markdown and JSON frontmatter remain the only source of truth.
- The index remains rebuildable acceleration data.
- This subsystem introduces no daemon, watcher, Web UI, TUI, vector store, or
  background service.
- Search quality improves through application-side tokenization rather than
  SQLite-specific Chinese tokenizer extensions.

The target architecture is:

```text
filesystem documents
-> scan + metadata normalize
-> gse tokenization + technical term extraction
-> SQLite structured tables + FTS5
-> search/context/review/doctor consumers
```

## Why Change

The current index is a flat JSON database rebuilt from Markdown and JSON files.
It is simple and auditable, but it now has three practical limits:

1. Search is still linear over loaded entries and uses basic substring matching.
2. Index freshness depends on explicit rebuilds, so stale results can surface
   until `worktrail index rebuild` is run.
3. Chinese queries need real tokenization; SQLite default tokenizers alone are
   not enough for mixed Chinese, English, command, and identifier content.

The new design should solve those limits without changing Worktrail's safety
model.

## Goals

- Support Chinese document and query tokenization using `gse`.
- Preserve explicit `project` and `user` scope behavior.
- Improve search relevance for mixed content:
  - Chinese natural language
  - English technical terms
  - commands
  - file paths
  - `snake_case`, `kebab-case`, and similar identifiers
- Keep the index fully local and rebuildable.
- Support lightweight incremental refresh before read commands.
- Reuse as much existing command surface and `contextpack` behavior as possible.

## Non-Goals

These non-goals apply to the lexical index subsystem described here. Opt-in
local semantic recall is specified separately in
`worktrail-local-semantic-recall-architecture.md`.

- No embedding or vector similarity search in this lexical path.
- No background indexing process for this lexical path.
- No SQLite custom tokenizer extension requirement.
- No change to Markdown as source of truth.
- No fully automatic ranking that overrides governance metadata.
- No cross-machine sync changes in this design.

## Design Principles

### Markdown Remains Source Of Truth

SQLite stores only derived state. If the database is deleted or corrupted,
Worktrail must be able to rebuild it from the filesystem.

### Chinese Tokenization Lives In Go

Chinese search quality should not depend on SQLite loadable extensions or
driver-specific tokenizer hooks. Tokenization happens in Go using `gse`, then
tokenized fields are written into FTS5.

### Governance Metadata Still Matters

FTS improves text recall, but final ranking must still consider Worktrail's
knowledge semantics such as `source_of_truth`, `active`, `lifecycle`, and
supersession state.

### Scope Separation Stays Explicit

Project and user knowledge remain physically separate indexes, matching the
existing CLI and mental model.

## Current State

Today `internal/index` scans scope roots, normalizes metadata, and writes a JSON
index database plus manifest. Search behavior is:

- load all indexed entries
- filter by scope/type/topic/tag
- perform case-insensitive substring checks against title and content
- apply heuristic scoring in Go

This keeps the implementation simple but does not scale well for:

- large transcript and evidence bodies
- Chinese search
- richer topic-centric navigation
- automatic freshness repair

## Alternatives Considered

### Alternative A: Keep JSON Index And Add Better Tokenization

Pros:

- minimal storage migration
- lowest short-term implementation risk

Cons:

- still requires linear scans for search
- harder to support incremental refresh and structured query evolution
- does not give a strong long-term base for tags, edges, and health checks

Rejected because it improves tokenization but leaves the storage and query model
too limited.

### Alternative B: SQLite With A Custom Chinese Tokenizer Extension

Pros:

- moves tokenization closer to the FTS engine
- may reduce some application-side query builder work

Cons:

- couples Worktrail to driver- and platform-specific SQLite extension behavior
- complicates release packaging and local `go install`
- makes testing and portability worse for a local-first CLI

Rejected because it conflicts with Worktrail's portability and simplicity
constraints.

### Alternative C: Bleve/Bluge Plus Separate Metadata Store

Pros:

- good text-search primitives
- avoids some direct FTS tuning work

Cons:

- splits the system into two different query models
- makes governance-heavy queries less natural than SQL tables
- adds more moving parts than needed for Worktrail's current scope

Rejected because Worktrail needs a unified local index for both text recall and
governance metadata, not a search engine plus a separate metadata layer.

## Proposed Architecture

### Storage Layout

Each scope gets its own SQLite database:

- project scope: `.worktrail/index/index.sqlite`
- user scope: `<user-worktrail-root>/index/index.sqlite`

SQLite is the only authoritative acceleration layer for indexing commands.

Status and diff JSON continue to expose machine-readable fields such as
`index_path`, which points to `index.sqlite`. Legacy JSON `index.db` and
`manifest.json` artifacts are not read or written.

### Driver And Build Contract

The first implementation should choose one supported SQLite driver and validate
FTS5 on every release target instead of trying to support multiple drivers in
the initial cut.

Preferred starting point:

- `modernc.org/sqlite` because it is pure Go and aligns with Worktrail's local
  CLI distribution model

Required constraints:

- FTS5 support must be verified in automated tests on supported platforms
- the SQLite integration must remain isolated behind the index storage layer so
  the driver can be replaced later if validation reveals a platform-specific
  issue
- Worktrail must not silently downgrade to a weaker search engine when FTS5 is
  unavailable

### Internal Layers

The index package should be split into focused layers:

- `scan`: filesystem walk, Markdown parsing, metadata normalization, `Entry`
  creation
- `tokenize`: text normalization, `gse` segmentation, identifier extraction,
  dictionary loading
- `store`: SQLite schema migration, upsert, delete, refresh, load
- `query`: FTS query building, structured filtering, ranking
- `facade`: keep public functions such as `Rebuild`, `Status`, `Diff`, and
  `Search`

This preserves current CLI call sites while allowing storage and search internals
to evolve.

## Tokenization Design

### Why `gse`

`gse` is the selected tokenizer because it is pure Go, avoids CGO, supports
custom dictionaries, and fits Worktrail's cross-platform CLI distribution model.

The tokenizer is a component, not the whole search pipeline.

### Tokenizer Interface

The implementation should sit behind a small interface so the index pipeline is
not tightly bound to `gse` internals:

```go
type Tokenizer interface {
    TokenizeDocument(doc DocumentForTokenize) TokenizedDocument
    TokenizeQuery(query string) TokenizedQuery
    LoadBaseDictionary(words []string) error
    LoadProjectDictionary(words []string) error
}
```

This keeps room for future tokenizer swaps without rewriting SQLite and FTS
logic.

### Document Normalization

Before tokenization, both documents and queries should pass through a shared
normalization pipeline:

- Unicode normalization
- full-width to half-width conversion where useful
- lowercase English
- whitespace collapse
- punctuation normalization
- bounded content extraction for very large files when producing excerpts

### Chinese Segmentation

Natural language text in titles, body content, and topic-like fields should be
segmented with `gse`.

### Technical Term Extraction

Chinese segmentation alone is not enough for Worktrail search. A second
application-side pass should extract technical terms from:

- file paths
- commands
- `snake_case` identifiers
- `kebab-case` identifiers
- CamelCase-like strings
- topic slugs
- tags
- file stems

For example, `source_of_truth` should produce both the full identifier and
split-friendly terms such as `source`, `truth`, and `source of truth`.

### Dictionary Strategy

Use two dictionary layers:

- base dictionary:
  - built-in Worktrail terminology
  - command names
  - governance keywords
- project dictionary:
  - topics
  - tags
  - rule names
  - integration names
  - prompt names
  - stable file stems and slug-like names

The project dictionary should be derived during rebuild or refresh rather than
requiring a manually curated file for the first version.

## SQLite Schema

### Main Table: `entries`

The main table stores structured metadata and refresh bookkeeping.

Suggested columns:

- `id`
- `scope`
- `path`
- `object_kind`
- `type`
- `title`
- `topic`
- `status`
- `stage`
- `lifecycle`
- `source_of_truth`
- `active`
- `candidate_type`
- `updated_at`
- `file_mtime`
- `file_size`
- `content_hash`
- `excerpt`
- `raw_meta_json`

Important properties:

- `path` must be unique within the scope database.
- frequently filtered fields should be real columns, not hidden inside JSON
- `raw_meta_json` remains as a compatibility/debugging escape hatch

### Content Materialization And Caller Compatibility

The current `index.Entry` contract includes `Content`, and existing callers use
it in bounded ways. In particular, `contextpack` renders short summaries for
`state` and `handoff` entries from `Entry.Content`, and search supports
`IncludeContent`.

To preserve those contracts without storing every full body in SQLite:

- `entries.excerpt` stores the default bounded preview text
- search recall and ranking use tokenized fields, not the raw body
- callers that explicitly need `Entry.Content` should hydrate it from the source
  file after the result set is selected
- the first implementation should limit body hydration to paths and entry types
  that already depend on it, especially `state`, `handoff`, and explicit
  `IncludeContent` reads

This keeps the database smaller while preserving behavior expected by current
callers.

### Tags Table: `entry_tags`

`entry_tags(entry_id, tag)` stores tags in normalized form for filtering and
future ranking.

### Edges Table: `entry_edges`

`entry_edges(from_entry_id, edge_type, to_path, to_entry_id)` stores structured
relationships such as:

- `supersedes`
- `superseded_by`
- future source trace edges such as `source_evidence`

This table is important for `doctor`, context ordering, and future topic-cluster
views.

### Index State Table: `index_state`

`index_state(key, value)` stores metadata such as:

- schema version
- scope
- generated time
- last refresh time

## FTS5 Design

### FTS Stores Tokenized Text, Not Raw Chinese Parsing Logic

FTS5 should receive already-normalized and tokenized text. SQLite is used as the
search engine and storage engine, but not as the source of Chinese segmentation
logic.

Suggested FTS columns:

- `title_terms`
- `body_terms`
- `topic_terms`
- `tag_terms`
- `ident_terms`

These columns should contain space-separated normalized terms produced by the Go
token pipeline.

### Why Not A SQLite Custom Chinese Tokenizer

This design intentionally avoids SQLite tokenizer extensions because they add:

- driver coupling
- extension loading complexity
- platform-specific behavior
- harder distribution for a local CLI tool

The Go-side token pipeline is easier to test, version, and evolve.

## Query Model

### Search Flow

`worktrail search` should follow this high-level flow:

1. tokenize the incoming query with the same normalization pipeline
2. build an FTS `MATCH` expression
3. retrieve candidate rows from FTS5
4. join with `entries`
5. apply structured filters:
   - scope
   - type
   - topic
   - tag
6. re-rank results with Worktrail-specific scoring

### Ranking

Text relevance should not be the only score. Final ranking should combine:

- FTS relevance, for example `bm25`
- `source_of_truth` boost
- `active` boost
- stage alignment boost when context requests a stage
- down-rank for superseded or non-current lifecycle entries
- small freshness boost from recent updates

This keeps search behavior aligned with knowledge quality and governance.

### Short Query Fallback

Very short Chinese queries can be noisy. The design should reserve a bounded
fallback path for short queries when FTS recall is weak, for example:

- exact title match
- exact tag match
- limited identifier match

This fallback should remain controlled and should not replace the main FTS path.

## Refresh And Rebuild Model

### Explicit Rebuild

`worktrail index rebuild` still performs a full rescan and full database rebuild.
This remains the recovery path and the easiest correctness baseline.

### Incremental Refresh

Before read-heavy commands such as `search` and `context`, Worktrail should run a
lightweight refresh:

- detect new files
- detect deleted files
- detect changed files by `mtime`, `size`, and optionally `content_hash`
- re-tokenize and upsert only affected entries

This improves freshness without introducing a daemon.

### Status And Diff

`status`, `diff`, and `health` should continue to exist, but internally they
compare SQLite state with the filesystem instead of comparing two JSON snapshots.

### Failure And Degradation

The design must define explicit recovery behavior:

- missing database: rebuild automatically on read commands, matching current
  behavior
- corrupt database or failed schema migration: move the broken database aside,
  rebuild once, and return an actionable error if rebuild also fails
- FTS5 unavailable on the selected build target: fail clearly with a doctor or
  runtime hint; do not silently fall back to substring search
- refresh lock contention: use bounded retries and short transactions; if
  refresh cannot acquire the write lock, continue with the last readable index
  and surface stale/index-health guidance instead of hanging

Rebuild remains the correctness oracle and the primary recovery path.

## Compatibility With Existing Callers

The goal is to preserve the current high-level contracts used by CLI and
`contextpack`.

The following public behavior should remain:

- `worktrail index rebuild`
- `worktrail index status`
- `worktrail index diff`
- `worktrail search`
- `worktrail context`

The `contextpack` package should keep consuming normalized `index.Entry`
instances. The storage backend changes first; section ordering and context
selection logic should remain mostly intact until targeted follow-up changes are
needed.

## Impact Surface

The main implementation impact is expected in:

- `internal/index`
- `internal/app/context_index.go`
- `internal/contextpack`
- index-related tests under `internal/index`

Secondary validation impact is expected in:

- documentation and troubleshooting references to `index.db`
- commands and tests that currently assume JSON-backed index status output

No changes to Markdown/frontmatter authoring format are part of this design.

## Migration Plan

### Phase 1: Refactor Current Index Internals

- separate scan/build logic from JSON storage logic
- keep current behavior unchanged
- add stronger tests around `Entry` normalization

### Phase 2: Introduce Token Pipeline

- add tokenizer interface
- implement `gse` tokenizer
- add normalization and technical term extraction
- add golden tests for token output

### Phase 3: Add SQLite Storage And FTS5

- create schema migrations
- implement rebuild into SQLite
- implement load/search from SQLite
- keep CLI surface unchanged

### Phase 4: Incremental Refresh And Ranking

- add refresh before `search` and `context`
- replace substring matching with FTS retrieval plus business rerank
- keep diff and health reports user-readable

### Phase 5: Follow-Up Improvements

- use `entry_edges` more aggressively in `doctor`
- improve topic-cluster navigation for context
- evaluate whether evidence documents need more aggressive excerpting or term
  limits

## Validation Plan

### Tokenization Tests

Add golden tests covering:

- Chinese sentences
- Chinese and English mixed text
- commands such as `worktrail index rebuild`
- identifiers such as `source_of_truth`
- paths and topic slugs
- punctuation and width normalization

### Search Integration Tests

Use a small fixture corpus to verify:

- Chinese queries return expected results
- mixed Chinese/English terms work
- topic and tag filters still work
- `source_of_truth` entries rank above weaker matches
- superseded entries rank lower than current ones

### Refresh Tests

Verify that:

- new files are indexed without full rebuild
- deleted files disappear from results
- changed files update excerpts and FTS content
- stale reporting remains correct

### Performance Checks

Measure:

- rebuild time on representative repositories
- refresh time for small and moderate diffs
- search latency for common short queries
- database growth for evidence-heavy repositories

## Risks

### Over-Indexing Large Bodies

Transcript-like or evidence-heavy files can bloat the database. This design
should prefer bounded excerpts in `entries` and controlled token generation for
very large bodies.

### Dictionary Noise

If project-derived dictionaries are too broad, search quality can get worse
instead of better. The first version should keep derived vocabulary bounded and
traceable.

### Query Builder Complexity

If the query builder becomes too clever, debugging search behavior becomes hard.
The first release should favor deterministic and explainable expansion rules.

### Refresh Correctness

Incremental refresh must not silently drift from full rebuild semantics.
Rebuild remains the correctness oracle, and tests should compare refresh results
with rebuild results on the same fixture set.

## Open Questions

1. Should very large evidence files be tokenized in full, or should they be
   capped to reduce database growth?
2. Should the project dictionary be purely derived, or should Worktrail support
   an optional checked-in domain dictionary later?
3. Resolved: `search --scope all` merges project and user results in Go,
   ranks them globally by score, and returns the top 20. Cross-database
   `ATTACH` is not planned for the first release.

## Decision

Adopt a dual-scope SQLite index with FTS5 and a Go-side token pipeline built on
`gse`.

The index remains rebuildable local acceleration data. Chinese segmentation and
technical term normalization live in Go, not in SQLite extensions. Structured
metadata and governance semantics continue to shape final result quality.


## Migration provenance

Distilled from `docs/worktrail-sqlite-gse-index-design.md`. The source remains in `docs/` until this candidate is promoted and inbound references are repaired.
