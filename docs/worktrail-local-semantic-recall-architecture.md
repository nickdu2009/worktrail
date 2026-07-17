# Worktrail Local Semantic Recall Architecture

Last updated: 2026-07-15

Status: proposed; design direction agreed, implementation not started

Architecture scale: subsystem

## Summary

This document defines the target architecture for adding local semantic recall
to Worktrail.

The selected stack is:

```text
Markdown knowledge
-> deterministic structural chunks
-> BGE-M3 dense embeddings
-> pinned llama.app `llama` runtime
-> sqlite-vec generation index
-> FTS5 + vector recall
-> reciprocal-rank fusion
-> Worktrail governance ranking
-> source-grounded results
```

The design intentionally keeps Worktrail's current safety model:

- Markdown and frontmatter remain the source of truth.
- Semantic indexes are derived, local, and fully rebuildable.
- Vector similarity never promotes, merges, retires, or otherwise mutates
  knowledge.
- Formal knowledge, candidates, evidence, handoffs, state, and runtime records
  keep their existing governance semantics.
- Search degradation is visible rather than silent.
- Model, runtime, chunker, and storage versions are traceable. A semantic
  profile mismatch causes a clean rebuild rather than an in-place migration.

The initial semantic implementation uses only BGE-M3 dense embeddings. BGE-M3
sparse output, ColBERT multi-vector retrieval, reranking, and approximate-nearest
neighbor indexes are outside the first release.

The compatibility boundary is intentionally narrow:

- existing Markdown, frontmatter, lexical search, Context Pack sections, and
  public command behavior remain compatible;
- semantic generations are internal derived data and have no backward-reader or
  migration guarantee;
- a Worktrail version either recognizes the exact semantic profile or marks it
  stale and rebuilds it;
- failed or unavailable semantic recall degrades visibly to the existing
  lexical path.

## Relationship To Existing Architecture

The implemented
[SQLite + GSE index design](worktrail-sqlite-gse-index-design.md) remains the
deterministic lexical and metadata baseline. It explicitly excluded vector
search and background services from its original scope.

This proposal is an explicit Worktrail v1.0.0 capability, not a replacement for
the baseline:

- the existing entry-level SQLite and FTS5 index remains available;
- the Go-side GSE and identifier token pipeline remains the lexical foundation;
- semantic recall adds profile-specific chunk, FTS, and vector generations;
- a user-level local inference process is introduced only for semantic
  operations;
- deleting every semantic artifact restores the existing lexical-only behavior.

This design also crosses two boundaries described as non-goals in the original
v1 vision: vector search and a background process. It should therefore ship as
an explicit product capability with installation, resource, rebuild, and
degradation contracts rather than as an invisible patch-level optimization.

## Product Context

Worktrail primarily serves individual developers. Switching between Cursor,
Claude Code, Codex, and other coding agents is a frequent workflow. Worktrail is
both:

- a local project knowledge base; and
- a continuity layer that helps a new agent recover the relevant task,
  decision, evidence, and next-step context.

Semantic recall is useful when the user's wording does not exactly match the
stored document. It must complement, not replace, exact lookup for paths,
commands, identifiers, tags, and governance fields.

## Goals

- Improve Chinese, English, mixed-language, and code-related recall.
- Keep all inference, model files, vectors, and query text local.
- Preserve exact FTS5 lookup and structured metadata filtering.
- Produce source paths and section-level citations for every result.
- Support deterministic chunking and incremental re-embedding.
- Avoid loading the model for commands that do not need semantic recall.
- Reuse a loaded model across frequent agent calls and multiple projects.
- Install a tested runtime bundle only through `worktrail init --semantic`.
- Use the official prebuilt llama.app `llama` program without building
  llama.cpp or requiring Python, CMake, or a system-wide runtime installation.
- Make runtime recovery automatic without adding a health-check request to
  every successful query.
- Support side-by-side rebuilds, atomic activation, and safe cleanup.
- Keep semantic data derived and safe to delete.

## Non-Goals

The first implementation does not include:

- cloud embedding APIs;
- a required Ollama installation;
- a required Python or FlagEmbedding runtime;
- downloading or compiling llama.cpp source during installation;
- executing `curl | sh` or another downloaded installer;
- semantic runtime support outside macOS Apple M1-M5 in v1;
- Qdrant, Milvus, or another standalone vector database;
- BGE-M3 learned sparse vectors;
- BGE-M3 ColBERT token vectors;
- a cross-encoder reranker;
- experimental sqlite-vec DiskANN or IVF indexes;
- LLM-generated summaries or LLM-driven chunk boundaries;
- automatic semantic judgment of knowledge quality;
- silent model or runtime upgrades;
- backward reading or migration of old semantic generation schemas;
- vector reuse across different semantic profile IDs;
- no-re-embedding semantic rollback;
- hybrid `scope all` retrieval across different semantic profiles;
- semantic support after downgrading to an older Worktrail binary;
- semantic similarity overriding lifecycle, source-of-truth, or supersession
  rules.

## Confirmed Technology Decisions

### Embedding Model

Use the official `ggml-org/bge-m3-Q8_0-GGUF` quantization of
`BAAI/bge-m3` for the initial semantic profile:

- model repository: `ggml-org/bge-m3-Q8_0-GGUF`;
- model revision: `9eba04c5d75ba5a1595e45de734d36bef4e5cb98`;
- model file: `bge-m3-q8_0.gguf`;
- model file size: `634553760` bytes;
- model SHA-256:
  `aa473d51f451a22f0fcf39ba3330c14bed38a385712b1113440f69df4047a173`;
- original model: `BAAI/bge-m3`;
- parameter scale: approximately 568M;
- output dimension: 1024;
- maximum model input: 8192 tokens;
- pooling: CLS;
- normalized output: required;
- distance metric: cosine;
- initial artifact format: GGUF Q8_0;
- initial retrieval mode: dense only.

The 8192-token model limit is a safety ceiling, not the target chunk size.
Smaller, topic-coherent chunks provide more precise retrieval and citations.

The pinned Q8_0 file is the v1 release candidate, not an unverified substitute
for the original model. Before acceptance, its dense output must pass parity
evaluation against `BAAI/bge-m3` through FlagEmbedding. Quantizing model weights
does not reduce the 1024-dimensional output or its vector-index storage.

The fixed model download URL is:

```text
https://huggingface.co/ggml-org/bge-m3-Q8_0-GGUF/resolve/9eba04c5d75ba5a1595e45de734d36bef4e5cb98/bge-m3-q8_0.gguf?download=true
```

### Inference Runtime

Use the official prebuilt unified llama.app `llama` program, exposed through
`llama serve`.

Worktrail integrates through the local HTTP embedding API rather than linking
the native runtime through CGo. Worktrail does not download or compile
llama.cpp source and does not require users to install Python, CMake, or a
system-wide `llama` executable.

Reasons for selecting the official llama.app distribution:

- native GGUF support;
- optimized Apple Silicon and Metal variants;
- no Python environment;
- one official `llama` program with a server surface;
- local embedding and tokenization APIs;
- explicit runtime artifacts that can be checksummed and pinned;
- no mutation of the user's `PATH`.

The official distribution references are:

- <https://llama.app/>;
- <https://llama.app/install.sh>;
- <https://github.com/ggml-org/llama-install.sh>;
- <https://huggingface.co/buckets/ggml-org/install.sh>;
- <https://huggingface.co/buckets/ggml-org/install.sh/resolve/latest>.

These references are discovery and provenance sources. Worktrail never executes
the remote installer. Before a Worktrail release, the current llama.app pointer
is resolved to one tested version. The design keeps that value as
`<pinned-llama-app-version>` until the release gate passes.

The official installer selects different Metal artifacts for Apple M1-M5.
Worktrail v1 mirrors only this bounded selection logic. M1 is the only
`verified` variant and passes the complete physical-hardware gate. M2-M5 are
opt-in `experimental` variants with their own pinned official artifact and no
cross-chip fallback. Installation must run bounded local artifact/API
self-checks before an experimental variant can be used. Experimental variants
make no compatible or verified, performance, privacy, minimum-macOS, or
operational-support claim, and do not require pre-release per-chip self-check
reports. A local `sysctl`-based probe chooses the matching entry. A18 and every
non-Darwin target return `semantic_platform_unsupported`.

Official runtime artifacts are distributed as `llama-app.zst`. Worktrail uses
the pinned pure-Go `github.com/klauspost/compress/zstd` decoder to
decompress in-process. It does not download or execute the installer's
`unzstd` helper. Both compressed and final executable sizes and digests are
validated before activation.

### Vector Storage

Use sqlite-vec through the existing `modernc.org/sqlite` distribution.

The current dependency already includes a generated sqlite-vec package for
Darwin arm64. This preserves Worktrail's CGO-free SQLite integration.

BGE-M3's 1024 dimensions are within sqlite-vec's current 8192-dimension limit.
The initial index stores normalized float32 vectors and uses exact cosine KNN.

### Hybrid Retrieval

Use three independent signals:

1. chunk-level lexical retrieval through the existing Go token pipeline and
   FTS5;
2. chunk-level dense semantic retrieval through BGE-M3 and sqlite-vec;
3. Worktrail metadata and graph signals for governance-aware reranking.

Combine lexical and semantic ranked lists with reciprocal rank fusion (RRF).
Apply governance, source quality, supersession, scope, and diversity logic after
candidate generation.

## Alternatives Considered

### Granite Embedding Multilingual R2

Granite 97M R2 is substantially smaller and remains a reasonable future
lightweight profile. It was not selected because the agreed priority is
BGE-M3's mature multilingual and code retrieval behavior and its possible
future dense, sparse, and multi-vector evolution.

### Qwen3 Embedding

Qwen3 Embedding offers strong multilingual and code retrieval, but it is heavier
and benefits from query instructions. It is not needed for the first local
profile.

### Ollama

Ollama provides the easiest local embedding API, but it adds a broader daemon
and model-management dependency. Worktrail needs only a pinned embedding
runtime and chooses to manage the official llama.app `llama serve` process
directly.

### Build llama.cpp From Source

Building llama.cpp would expose Worktrail users and CI to compiler, CMake,
backend, and platform variation. It is rejected for v1 because llama.app
already publishes hardware-specific official binaries that can be pinned and
verified.

### Execute The Official Installer

The official installer provides useful provenance and hardware-selection logic,
but executing a downloaded shell script would add a mutable supply-chain and
filesystem boundary. Worktrail instead records equivalent immutable artifact
metadata and performs download, zstd decompression, verification, and
installation itself.

### ONNX Runtime

ONNX Runtime could run BGE-M3 in-process, but Worktrail would need to own model
export, tokenizer behavior, pooling, normalization, native runtime packaging,
and Go binding compatibility. It remains a possible future runtime after the
llama.app profile is validated.

### One Vector Table Inside The Existing Index

Adding a single `vec0` table directly to the existing `index.sqlite` is simpler
initially but makes side-by-side chunker, model, dimension, and sqlite-vec
changes difficult. Profile-specific generation databases are selected so a new
index can be built and validated without mutating the active one. They are an
atomic-rebuild boundary, not a backward-compatibility promise.

## Architecture Overview

```text
                         user cache
                +-------------------------+
                | immutable bundle store  |
                | llama.app llama + GGUF  |
                +------------+------------+
                             |
                             v
                +-------------------------+
                | runtime supervisor      |
                | one daemon per bundle   |
                | needed by active scopes |
                +------------+------------+
                             |
                 tokenize + embeddings
                             |
                             v
filesystem -> scanner -> structural chunker -> recall generation builder
     |                                             |
     |                                             v
     |                              scope-local generation SQLite
     |                              chunks + chunk_fts + vec0
     v                                             |
existing entry SQLite + FTS5 ----------------------+
                             |
                             v
                 hybrid retrieval + governance
                             |
                             v
                  cited, explainable results
```

Dependency direction:

- application commands depend on a recall facade;
- the recall facade depends on lexical, semantic, and governance interfaces;
- semantic indexing depends on the chunker, runtime client, and generation
  store;
- runtime process management does not depend on project index internals;
- storage and runtime implementations remain replaceable behind narrow
  interfaces.

## Component Decomposition

### Semantic Bundle Manager

Responsibilities:

- resolve the recommended bundle for the current Worktrail version;
- derive a content-addressed bundle ID from the canonical trusted manifest;
- select the trusted llama.app Metal artifact for the detected Apple M-series
  chip;
- install the pinned BGE-M3 Q8_0 GGUF artifact;
- stream-decompress the pinned `llama-app.zst` with a pure-Go decoder;
- verify compressed and executable size, checksum, attribution, profile
  identity, and trust metadata;
- stage and verify the complete bundle before one atomic directory activation;
- keep immutable, content-addressed bundles side by side;
- expose bundle status without loading the model;
- support explicit upgrade/rebuild and cache-cleanup planning.

The bundle manager writes only to the user-level Worktrail cache. Model files
are not duplicated into every project.

### Runtime Supervisor

Responsibilities:

- discover a healthy daemon for a requested bundle;
- start `llama serve` when the direct API fast path fails recoverably;
- serialize concurrent starts with a per-bundle user lock;
- record PID, endpoint, bundle identity, runtime version, and log path;
- reject a process whose model identity does not match the requested bundle;
- stop, restart, and report runtime status;
- clean stale process descriptors;
- bound the number of concurrently loaded bundles.

The common case is one daemon shared by every project using the same bundle.
During a rebuild, the active bundle and candidate bundle may coexist
temporarily, but one query never mixes them. Runtime state is keyed by bundle
ID, not represented by a single global PID.

### Structured Chunker

Responsibilities:

- parse Markdown through the existing Goldmark dependency;
- separate frontmatter from content;
- construct heading breadcrumbs;
- preserve meaningful paragraph, list, code, quote, and table boundaries;
- apply exact BGE-M3 token budgets through the pinned llama.app tokenization
  API;
- add stable semantic context;
- produce chunk IDs, content hashes, neighbor links, and source offsets;
- remain deterministic for the same source and chunker version.

### Semantic Indexer

Responsibilities:

- select indexable objects according to scope and governance policy;
- refresh only added, changed, or deleted chunks;
- batch embedding calls;
- write chunk, lexical, vector, and profile metadata transactionally;
- build new generations beside the active generation;
- compare incremental refresh output with full rebuild semantics;
- activate a generation only after validation succeeds.

### Recall Generation Store

Responsibilities:

- own one immutable or append-safe generation database per recall profile;
- store chunks, chunk-level FTS terms, vectors, and profile metadata;
- perform exact KNN and metadata-compatible filtering;
- expose a stable query API independent of sqlite-vec SQL details;
- remain safe to delete and rebuild.

### Hybrid Retriever

Responsibilities:

- parse scope and structured filters;
- query lexical and semantic lanes;
- generate one query vector for the exact active semantic profile;
- fuse ranked lists with RRF;
- apply Worktrail governance and graph reranking;
- enforce per-document diversity;
- expand adjacent chunks only after ranking;
- return paths, headings, profile IDs, scores, and lane diagnostics.

## Runtime Topology And Lifecycle

### Process Topology

`llama serve` runs as a detached user-level process. It is not tied to the
lifetime of one `worktrail search` command.

Each runtime descriptor is keyed by immutable bundle ID and records:

- PID;
- process start time or platform-equivalent process identity;
- loopback endpoint;
- llama.app version, runtime artifact checksum, and Apple chip variant;
- model and GGUF checksum;
- model alias;
- API-key file path;
- last successful request time;
- log path;
- readiness state.

The server binds only to `127.0.0.1`. It must not listen on a LAN interface by
default. Worktrail starts it with a random per-process API key stored in a
current-user-only file and an alias equal to the immutable bundle ID.

Endpoint allocation uses a user-level allocation lock shared across bundles.
Under that lock, Worktrail asks the operating system for a loopback ephemeral
port, releases the probe listener immediately before starting `llama serve`,
and applies a bounded retry policy if another process wins the bind race. It
never kills an unknown process occupying a candidate port.

### API-First Recovery

Normal queries do not perform a separate health request.

```text
read the cached runtime descriptor
-> call the embedding API directly with the API key
-> revalidate bundle, daemon, and active-generation identity
-> verify the returned model alias and embedding dimension
-> identity matches: return
-> recoverable transport failure: acquire the bundle start lock
-> retry once in case another agent already recovered the server
-> inspect and clean stale state
-> start `llama serve`
-> wait for bounded readiness
-> retry the original embedding request once
```

Recoverable startup signals include:

- connection refused;
- missing endpoint;
- closed connection before a request is accepted;
- stale or missing PID state;
- a server explicitly reporting that its model is still loading.

The following do not trigger a restart loop:

- HTTP 4xx responses;
- invalid or oversized input;
- model or profile mismatch;
- authentication or model-alias mismatch;
- incompatible API response shape;
- inference execution timeout;
- vector-store or ranking failure.

Authentication, alias, or process-identity mismatch invalidates Worktrail's
descriptor. Worktrail may acquire its lock and start the correct bundle once on
a newly selected endpoint, but it never retries, replaces, or kills the unknown
process occupying the old endpoint.

Connect timeout and inference timeout are separate. A slow inference must not be
treated as proof that the process is absent.

### Concurrency

Multiple agents may call Worktrail simultaneously. Startup uses:

1. a per-bundle inter-process lock;
2. a second direct API attempt after acquiring the lock;
3. one bounded start attempt;
4. one post-readiness retry.

This prevents a thundering herd from starting duplicate 1+ GB model processes.

### Lifetime And Resource Bounds

The initial behavior keeps a successfully started daemon alive across commands.
An idle timeout may be configurable, but it is not required for correctness.

Because a replacement generation may temporarily use a new bundle while the
old generation still serves requests, the supervisor must implement a resource
policy. The proposed first policy is:

- reuse a daemon only when its API key, bundle alias, runtime fingerprint, and
  embedding dimension match;
- allow active and candidate bundles to coexist only while building a
  replacement generation;
- after activation, drain and stop the old daemon; its old generation is not
  retained for rollback;
- stop an idle old-bundle daemon before starting an unrelated third bundle;
- never stop a daemon with in-flight requests;
- preserve installed bundle files even when the process is stopped.

## Installation And Bundle Contract

### Installation Trigger

Package-manager installation of the Worktrail binary must not silently download
a large model. Core initialization and semantic installation are separate
operations.

Core `worktrail init`:

- completes core initialization only;
- never prompts for or starts semantic installation;
- does not access the network by default;
- does not build a semantic generation.

`worktrail init --semantic` is the sole semantic-installation entry point. It
shows the expected model/runtime size and user-cache destination, preserves
successful core initialization if semantic installation fails, does not build a
semantic generation, and prints `worktrail semantic rebuild --scope all` after
successful installation. `worktrail init --no-semantic` explicitly disables
semantic installation. The flags are mutually exclusive.

The installer must be injectable so tests use bounded local fixtures rather
than the real network or 634,553,760-byte model.

When installation is requested, Worktrail reports runtime/model download size,
peak disk use, and final location; checks available disk space; downloads to
temporary files on the destination filesystem; verifies trusted manifest size
and digests; stream-decompresses the runtime in-process; runs bounded offline
self-tests; and atomically activates the immutable bundle. The first operation
that needs tokenization or embedding starts the persistent daemon. Installation
alone never creates an index.

The installation sequence is:

1. complete core `worktrail init`;
2. require an explicit `--semantic` request; `--no-semantic` explicitly
   disables semantic installation;
3. show runtime/model download size, peak disk use, and installation location;
4. download immutable model and compressed runtime URLs to temporary files;
5. verify download size and SHA-256;
6. stream-decompress the runtime with the pinned pure-Go decoder and verify the
   final executable size and SHA-256;
7. assemble the complete model, runtime, manifest, licenses, and attribution in
   a sibling staging directory;
8. run bounded version, model-metadata, and offline capability self-tests;
9. fsync the files and staging directory, then atomically rename the complete
   directory to the content-addressed bundle path;
10. verify and reuse an already-existing destination only when its complete
    manifest and artifact digests match;
11. leave all semantic generations absent;
12. print `worktrail semantic rebuild --scope all`.

### Immutable Manifest

Every bundle manifest includes at least:

```yaml
schema: worktrail.semantic-bundle.v1
bundle_id: <sha256-of-canonical-trusted-manifest>
model_repository: ggml-org/bge-m3-Q8_0-GGUF
model_revision: 9eba04c5d75ba5a1595e45de734d36bef4e5cb98
model_file: bge-m3-q8_0.gguf
model_url: https://huggingface.co/ggml-org/bge-m3-Q8_0-GGUF/resolve/9eba04c5d75ba5a1595e45de734d36bef4e5cb98/bge-m3-q8_0.gguf?download=true
model_size: 634553760
model_sha256: aa473d51f451a22f0fcf39ba3330c14bed38a385712b1113440f69df4047a173
original_model: BAAI/bge-m3
model_format: GGUF-Q8_0
dimension: 1024
context_length: 8192
pooling: cls
normalized: true
embedding_mode: dense
runtime_distribution: llama.app
runtime_version: <pinned-llama-app-version>
runtime_executable: llama
runtime_subcommand: serve
supported_platform: darwin-arm64
runtime_variants:
  - chip: m1
    support_level: verified
    compressed_url: <immutable official URL>
    compressed_size: <bytes>
    compressed_sha256: <digest>
    executable_size: <bytes>
    executable_sha256: <digest>
    minimum_macos: <verified version>
  - chip: m2
    support_level: experimental
    compressed_url: <immutable official URL>
    compressed_size: <bytes>
    compressed_sha256: <digest>
    executable_size: <bytes>
    executable_sha256: <digest>
  # M3-M5 use the same experimental per-chip artifact fields. They have no
  # minimum_macos field or physical-hardware release-evidence requirement.
semantic_api_version: 1
generation_schema_version: 1
model_space_id: <embedding-coordinate fingerprint>
```

`bundle_id` is the SHA-256 of a versioned canonical manifest representation
that excludes `bundle_id`, signatures, installation timestamps, and other
mutable local state. It includes the model identity, llama.app version, every
release-supported runtime variant and digest, API contract, licenses, and
attribution. Changing any trusted artifact or executable contract therefore
creates a different bundle path, daemon key, lock namespace, and model alias.
Golden fixtures fix the canonical field order and hash.

The model is fetched only from the immutable official URL above. The runtime
variant URLs are resolved from a specific llama.app version during release
preparation; Worktrail never resolves the mutable `latest` pointer during
installation. The manifest records the model and runtime licenses, attribution,
download sizes, and peak installation disk estimate for each Apple chip
variant. A minimum macOS version is recorded only for the verified M1 variant.

For v1, the canonical immutable manifest embedded in the formal Worktrail
release is the only runtime trust root. A separate release-attestation,
signature, or verifier is not a startup or production-installation
prerequisite. Ordinary release distribution integrity for the Worktrail tag and
binary remains required. A future independent update channel requires a new
trust design and is outside v1.

### Version Policy

Pinning does not prevent upgrades. It defines one immutable semantic coordinate
system. The first release deliberately does not migrate or backward-read old
semantic generations.

- `worktrail init --semantic` installs the recommended user-level bundle; it
  does not build a project or user generation. Plain `worktrail init` remains
  network-free by default, and `--no-semantic` explicitly disables installation.
- The semantic `model_space_id` is a canonical hash of the model repository,
  revision, file SHA-256, dimension, tokenizer, pooling, normalization, and
  query/document input templates.
- The full `recall_profile_id` is a canonical hash of `model_space_id`,
  llama.app version and executable digest, chunker, indexing policy, lexical
  pipeline, generation schema, vector metric, and sqlite-vec version.
- Worktrail uses a generation only with a runtime and query pipeline whose
  complete `recall_profile_id` matches it.
- Any attempted mismatch marks the generation stale for that request and
  requires a clean rebuild.
- Update discovery may report a newer bundle but does not activate it.
- Upgrade is explicit because it may download large files and rebuild vectors.
- No upgrade path copies vectors or assumes llama.app output compatibility
  across profile IDs.
- Changing to Qwen3-Embedding, EmbeddingGemma, or another model creates a new
  semantic profile and rebuilds from Markdown. Search and Context Pack
  interfaces do not change, and the old vector space is never migrated or
  queried together with the new one.

## Chunking Design

### Principle

Use structure first and token limits second. Do not use a blind fixed-width
sliding window.

A chunk should be a complete, independently understandable, source-citable
knowledge unit.

### Pipeline

```text
parse frontmatter and Markdown AST
-> create heading sections
-> create atomic block groups
-> merge small sibling blocks
-> split oversized blocks at semantic syntax boundaries
-> add stable context prefix
-> count exact BGE-M3 tokens
-> emit deterministic chunks and neighbor links
```

### Initial Token Budget

- target: 512 BGE-M3 tokens;
- preferred range: 320 to 640;
- hard maximum: 768;
- small-chunk threshold: 80;
- forced-split overlap: 64.

These are initial defaults, not universal truths. Retrieval evaluation may tune
them without changing the structure-first rule.

The context prefix counts toward the hard maximum.

### Boundary Rules

Paragraphs:

- keep a normal paragraph intact;
- split an oversized paragraph at sentence boundaries;
- use a token overlap only when a single logical block must be split.

Lists and procedures:

- keep an item intact;
- keep short numbered procedures together;
- split long lists between items, not inside an item.

Code blocks:

- keep a bounded code block intact;
- split oversized code by function or type boundary when detectable;
- otherwise split at blank lines, then bounded line groups;
- repeat language, file, and heading context for every emitted part.

Tables:

- keep a bounded table intact;
- split oversized tables by row;
- repeat the header row in every emitted chunk.

Headings:

- prefer one coherent heading section per chunk;
- merge a tiny section only with a sibling under the same parent;
- include the complete heading breadcrumb in every chunk.

### Semantic Context Prefix

The embedding input contains stable semantic context such as:

```text
title: Worktrail Local Semantic Recall Architecture
type: architecture
topic: recall
section: Chunking Design > Boundary Rules
path: docs/worktrail-local-semantic-recall-architecture.md

<chunk body>
```

Include:

- title;
- object type;
- topic and bounded tags;
- heading breadcrumb;
- stable relative path;
- body.

Do not embed volatile governance or runtime fields:

- timestamps;
- active process state;
- random runtime IDs;
- temporary lifecycle observations.

Those fields remain structured filters and ranking inputs.

### Identity And Incremental Refresh

Each chunk has:

- a source scope and entry ID;
- a deterministic logical chunk ID;
- heading breadcrumb and local order;
- content hash of the complete embedding input;
- previous and next chunk IDs;
- source offsets where available;
- chunker version.

The vector cache key includes the complete `recall_profile_id` and content hash.
Unchanged chunks reuse their vector only within that exact profile. Added,
changed, and deleted chunks are applied only while building a separate candidate
generation; the active generation is never updated in place.

Search and Context Pack may start a same-profile candidate refresh only when
source drift is within an ADR-approved count and time budget. This operation is
visible in diagnostics, never performs a full rebuild, and continues serving
the pinned active generation until candidate validation and atomic activation
succeed. Missing, corrupt, incompatible, or profile-stale generations never
trigger an implicit full rebuild; they return a stable reason and an explicit
`worktrail semantic rebuild --scope ...` repair command.

A changed heading may intentionally invalidate descendant embeddings because
the heading breadcrumb contributes semantic context.

### Neighbor Expansion

Do not create large overlaps at every natural boundary. After ranking, the
context assembler may attach the previous or next chunk from the same document.

This provides readable context while reducing duplicate vectors and preventing
one long document from occupying the entire result list.

## Data Architecture

### Source Of Truth

Markdown, JSON frontmatter, and existing Worktrail object semantics remain the
only durable source of truth.

Semantic bundle files and all recall databases are derived assets.

### Default Indexing Policy

The initial `indexing_policy_version` includes:

- formal project and user knowledge;
- active state;
- the latest valid handoff.

It excludes by default:

- pending candidates and evidence candidates;
- runtime sessions, checkpoints, and transient recovery records;
- archived or historical state;
- older handoffs;
- raw transcripts, imports, logs, and exports.

Selection occurs before chunking or embedding. Candidate and evidence search
may be added later as an explicit lane or separate profile; it must not silently
enter the default semantic corpus.

### User-Level Bundle Layout

Bundle files, mutable runtime state, and logs use separate operating-system
locations:

```text
<user-cache>/worktrail/
  semantic/
    bundles/
      <bundle-id>/
        manifest.json
        llama
        bge-m3-q8_0.gguf
        licenses/

<user-runtime>/worktrail/semantic/
  <bundle-id>/
    state.json
    start.lock
    api-key

<user-logs>/worktrail/semantic/
  <bundle-id>.log
```

Exact platform paths use platform-appropriate cache, runtime-state, and log
resolvers rather than hard-coded home-directory conventions. Runtime descriptor,
lock, and key files are current-user-only and are not stored solely in an
evictable model cache.

### Scope-Level Index Layout

Project scope:

```text
.worktrail/index/index.sqlite
.worktrail/index/semantic/
  active.json
  <generation-id>.sqlite
```

User scope follows the same layout under the user Worktrail root.

The existing `index.sqlite` owns entry-level metadata, edges, and baseline FTS.
Each semantic generation database owns a complete, internally consistent recall
profile:

- profile metadata;
- structural chunks;
- chunk-level lexical terms and FTS5 table;
- 1024-dimensional vectors;
- neighbor relationships;
- build and validation state.

Keeping chunks and chunk-level FTS in the generation database is required for
safe atomic rebuilds. A vector generation must never point to chunk boundaries
from another profile.

Candidate generations use writable connections only while building. Before
activation, Worktrail checkpoints and validates the database, closes every
writable handle, and thereafter opens that generation through a read-only
SQLite URI without running schema creation or repair SQL. A query acquires its
generation lease before opening the pinned generation and releases the lease
only after closing the connection. Pointer activation and old-generation
deletion use the same scope coordination boundary so a newly inactive database
cannot be removed before all pre-switch queries drain.

### Recall Profile Identity

The semantic model profile defines one embedding coordinate system:

- model repository, revision, GGUF checksum, and dimension;
- tokenizer, pooling, normalization, and query/document input templates;

Its canonical hash is `model_space_id`. Vectors with different
`model_space_id` values are never copied, compared, or queried together.

A recall profile adds the complete runtime and indexing contract:

- `model_space_id`;
- llama.app version, executable checksum, chip variant, and runtime API
  fingerprint;
- chunker version;
- indexing policy version;
- lexical normalization/tokenizer version;
- vector metric and sqlite-vec version;
- generation schema version.

The canonical hash of these fields is `recall_profile_id`. Build timestamp,
source scope, and source snapshot belong to generation metadata, not profile
identity. A generation is readable only with its exact runtime and query
profile. There is no old-schema reader or in-place migration; an unrecognized
schema or attempted profile mismatch is reported as `semantic_profile_stale`
and rebuilt into a new generation.

The active profile pointer is local derived state. If it is removed, Worktrail
can select the recommended bundle and rebuild rather than treating the pointer
as durable knowledge.

### Vector Table

Conceptual sqlite-vec schema:

```sql
CREATE VIRTUAL TABLE chunk_vectors USING vec0(
  chunk_rowid INTEGER PRIMARY KEY,
  embedding FLOAT[1024] distance_metric=cosine
);
```

The exact schema may include partition or metadata columns only after query
benchmarks justify them.

### Capacity

One float32 BGE-M3 vector uses approximately 4096 bytes before SQLite overhead:

- 10,000 chunks: about 39 MiB of raw vectors;
- 100,000 chunks: about 391 MiB of raw vectors.

Float16 or int8 vector storage is not selected initially. Any storage
quantization requires retrieval-quality evaluation independent of model-weight
quantization.

### Cross-Scope Queries

Project and user indexes remain physically separate. `scope all` continues to
merge results in Go.

Hybrid `scope all` requires every participating semantic scope to use the same
`recall_profile_id`. If profiles differ or one is stale:

- `auto` mode uses lexical retrieval across all scopes and reports
  `semantic_profile_mismatch_across_scopes`;
- `required` mode returns a typed error;
- Worktrail may offer or schedule a full rebuild of the stale scope.

The first release does not generate multiple query vectors or fuse semantic
rankings from different vector spaces.

## Retrieval And Ranking

### Query Flow

```text
parse query and filters
-> refresh the existing lexical index as required
-> check active semantic generation presence, compatibility, and freshness
-> run chunk FTS retrieval
-> obtain a profile-matched BGE-M3 query vector
-> run exact sqlite-vec KNN
-> fuse lexical and vector lists with RRF
-> hydrate entry governance metadata
-> apply scope, lifecycle, source, graph, and diversity rules
-> attach bounded neighbor context
-> return cited results and diagnostics
```

### Lexical Lane

The lexical lane uses the existing Go-side normalization, GSE tokenization, and
identifier extraction principles. It remains strongest for:

- exact titles;
- commands;
- paths;
- tags;
- `snake_case`, `kebab-case`, and similar identifiers;
- short known terms.

### Semantic Lane

The semantic lane handles paraphrase and cross-language similarity. It does not
decide knowledge authority.

Initial search retrieves a bounded semantic candidate set through exact KNN.
Top-K values must be chosen from evaluation rather than embedded as
architecture constants.

### Fusion

RRF is selected because lexical and vector scores are not directly comparable.
It combines rank positions without pretending BM25 and cosine distance share a
calibrated numeric scale.

Weights and candidate depths remain evaluation parameters. Result diagnostics
should preserve the lexical rank, semantic rank, and final rank.

### Governance Reranking

After fusion, Worktrail may:

- boost formal source-of-truth knowledge;
- boost active and current knowledge;
- down-rank superseded or retired entries;
- honor explicit scope, type, topic, and tag filters;
- use entry edges such as `supersedes` and `source_evidence`;
- limit repeated chunks from one document;
- keep evidence, candidate, and runtime lanes out of default results unless the
  calling workflow includes them.

Vector similarity must not bypass these rules.

### Result Contract

Existing `worktrail search` text output and JSON v1 (`Entry` plus `Score`) remain
stable. Hybrid retrieval is initially explicit:

```text
worktrail search "<query>" --semantic
worktrail search "<query>" --semantic --format json-v2
```

JSON v2 uses a versioned envelope with retrieval metadata and a `results` array.
Each result preserves the high-level `entry` and `score` contract and may add
chunk matches. It exposes enough information to audit recall:

- scope;
- entry ID and path;
- object type;
- heading breadcrumb;
- chunk ID;
- retrieval lanes used;
- lexical, semantic, and final rank information;
- active recall profile;
- degraded-mode warnings where applicable.

`--explain` may expose chunk and lane details in text mode without changing the
default columns. Error and degradation decisions use stable reason codes rather
than message parsing.

## Interface Contracts

The architecture fixes the following capabilities and initial command surface.
Implementation planning may add details but must not silently replace these
contracts.

### Bundle Operations

- install the recommended bundle;
- inspect installed and recommended versions;
- verify checksums and self-test state;
- plan and apply an upgrade;
- plan confirmed eviction of reinstallable, long-unused bundle caches.

### Runtime Operations

- start a requested bundle;
- stop or restart a requested bundle;
- report process, endpoint, model, and health;
- recover automatically from an absent or stale process;
- avoid duplicate starts under concurrency.

### Index Operations

- build a recall profile;
- incrementally refresh by building a candidate generation beside the immutable
  active generation;
- validate a profile;
- atomically activate a profile;
- remove only inactive generations.

### Proposed CLI Surface

The initial command contract is:

```text
worktrail init [--semantic | --no-semantic]
worktrail semantic status
worktrail semantic start
worktrail semantic stop
worktrail semantic restart
worktrail semantic rebuild --scope project|user|all
worktrail semantic upgrade --dry-run
worktrail semantic upgrade
worktrail semantic gc
worktrail search "<query>" --semantic [--format json-v2] [--explain]
worktrail context "<task>" --semantic
```

Existing lexical `search`, Context Pack rendering, and index command contracts
remain stable. Internally they may use the recall facade rather than embedding
runtime-specific logic.

`--semantic`, `--semantic=auto`, and `--semantic=required` are the only accepted
semantic flag forms. The ambiguous space-separated
`--semantic required` form is rejected. A missing generation never triggers an
implicit rebuild or starts the runtime; it returns
`semantic_generation_missing` plus the appropriate
`worktrail semantic rebuild --scope ...` repair command.

The refresh and repair policy is:

- bounded same-profile source drift may create a candidate refresh in `auto`
  or `required` mode while the current active generation remains readable;
- exceeding the bounded refresh budget reports `semantic_profile_stale` and
  requires explicit rebuild;
- missing, corrupt, incompatible, or profile-mismatched generations never
  trigger implicit full rebuild;
- `auto` degrades visibly to lexical retrieval when semantic work cannot
  complete;
- `required` returns a typed error and repair command instead of lexical-only
  success.

### Context Pack Integration Contract

Semantic recall may select only the knowledge portion of a Context Pack, such
as project/user knowledge, requirements, architecture, decisions, rules,
workflows, lessons, glossary, integration, and validation knowledge.

The following remain deterministic and retain their existing ordering and
budgets:

- active state;
- the existing handoff section, including its current limit of up to two
  handoffs;
- recovery;
- maintenance;
- evidence sections controlled by existing opt-in behavior.

Semantic similarity cannot remove, replace, or compete across budget with these
sections. If semantic recall is unavailable, Context Pack generation continues
with its current deterministic and lexical behavior.

Requirements have one explicit task/topic exception:

- with `--topic`, matching requirements are pinned first in their existing
  deterministic order; remaining requirement slots may be filled semantically
  from other topics;
- without `--topic`, no requirement is pinned and all requirements enter
  semantic selection;
- cross-topic fill applies only to requirements. Other knowledge sections still
  obey the topic filter before semantic ranking.

This does not change semantic indexing policy: each scope indexes only its latest
valid handoff, while Context Pack assembly may still present up to two
deterministic handoffs.

### Degradation Contract

Retrieval supports:

- `lexical`: run only the existing lexical path;
- `auto`: use available requested lanes and report any failure;
- `required`: fail when semantic recall cannot run exactly as requested.

Normal strategy selection, such as lexical mode or semantic not being enabled,
is distinct from operational failure. Stable failure reason codes include:

```text
semantic_disabled
semantic_platform_unsupported
semantic_bundle_missing
semantic_runtime_unavailable
semantic_runtime_identity_mismatch
semantic_runtime_capacity_exceeded
semantic_generation_missing
semantic_profile_stale
semantic_profile_mismatch_across_scopes
semantic_generation_incompatible
sqlite_vec_unavailable
fts_query_failed
```

FTS SQL failure must return a typed error to the recall facade. A linear or
other fallback may still be selected in `auto` mode, but the actual retrieval
lanes and `degraded_reasons` must be present in JSON v2 and summarized in text
diagnostics.

## Repository Impact Surface

The new subsystem should use a distinct package root such as:

```text
internal/semantic/
  bundle/
  daemon/
  chunk/
  generation/
  retrieve/
```

Ownership:

- `bundle`: manifest trust, installation, verification, upgrade, and cache
  cleanup;
- `daemon`: `llama serve` process, lock, identity handshake, and API client;
- `chunk`: Goldmark structural chunking and token budgets;
- `generation`: generation SQLite build, validation, activation, and rebuild;
- `retrieve`: lexical/vector lanes, RRF, governance, and diagnostics.

Existing module impacts:

- `internal/app`: init flags, semantic commands, search v2 formatting, and
  reason codes;
- `internal/index`: existing entry SQLite/GSE/FTS and typed lexical errors;
- `internal/contextpack`: semantic knowledge selection only;
- `internal/store`: core init followed by optional semantic installation;
- `internal/paths`: bundle, runtime-state, log, and generation paths;
- knowledge classification code: the versioned default indexing policy.

The existing `internal/runtime` package remains responsible for Worktrail
session, checkpoint, and recovery records. It must not also own inference
process supervision.

## Upgrade And Rebuild

### Upgrade Principle

Use side-by-side installation and indexing only as an atomic rebuild mechanism:
build, catch up, validate, and then activate. The first release does not provide
semantic-generation migration or no-re-embedding rollback.

Never mutate an active bundle or recall database in place.
No old semantic generation is retained as a rollback target. Before activation,
the currently active generation remains readable; after successful activation
and request draining, Worktrail automatically deletes it.

### Upgrade Flow

```text
resolve candidate bundle
-> show download, disk, and full-rebuild impact
-> download and verify immutable artifacts
-> start and identity-check the candidate runtime
-> verify release-attested parity metadata and run bounded local self-tests
-> record initial source snapshot S0
-> build a new recall generation beside the active one
-> rescan source as S1 and apply S0-to-S1 additions, changes, and deletions
-> repeat a bounded catch-up pass until the source snapshot converges
-> run retrieval and integrity validation
-> acquire a short activation lock and perform one final source diff
-> atomically switch the scope's active profile
-> drain unneeded old runtime requests
-> automatically remove the replaced generation after leases and build
   references drain
```

An interrupted or failed upgrade leaves the active profile unchanged.
If source changes do not converge within the bounded catch-up policy, Worktrail
does not activate the candidate and reports it stale.

### Strict Rebuild Policy

Any profile-field change, including llama.app runtime fingerprint, model artifact,
tokenizer, pooling, normalization, dimension, input template, chunker, indexing
policy, lexical pipeline, generation schema, or sqlite-vec version, creates a
different profile ID and requires a full rebuild.

Worktrail does not:

- open an unrecognized generation schema;
- copy vectors between profiles;
- compare golden vectors to waive a rebuild;
- query active and candidate generations together;
- promise semantic behavior after binary downgrade.

Before activation, the old active generation remains the automatic failure
fallback. After activation, recovery from a bad new profile is lexical-only or
an explicit full rebuild from Markdown using a corrected or previously selected
profile. Worktrail never switches the active pointer back to an old generation.

### Garbage Collection

Project or user generation cleanup may remove a generation only when:

- the current scope does not mark it active;
- no build process or query lease references it;
- it is a failed or otherwise inactive candidate selected by an explicit GC
  dry-run and confirmation.

The generation replaced by a successful activation is a special scoped cleanup:
it is deleted automatically as soon as its query leases and build references
drain, without entering the general GC inventory. It is never retained for
rollback. Failed or unrelated inactive candidates may be deleted only after
their validation diagnostics have been recorded and explicit GC confirmation
has been provided.

Bundle cleanup is cache eviction, not authoritative reference analysis.
Worktrail cannot reliably enumerate every historical project on disk. It may
remove a long-unused, reinstallable bundle after a dry-run and confirmation;
opening an old project later reports the missing bundle and offers explicit
reinstallation. Bundle cleanup never removes project knowledge or generation
metadata.

## Failure And Degradation

### Runtime Unavailable

Worktrail may continue with lexical recall, but the result must explicitly state
that semantic recall was unavailable. A caller that requires semantic recall
must be able to request strict failure.

There is no silent downgrade that presents lexical-only results as a successful
hybrid search.

### Missing Bundle

The normal path is installation through `worktrail init --semantic`. If
expected artifacts are missing:

- verify whether the bundle was intentionally disabled;
- report the missing artifact and expected size;
- allow one explicit or policy-approved repair installation;
- do not enter an unbounded download/start retry loop.

### Corrupt Or Incompatible Generation

- use the current generation only when its exact profile is recognized;
- move or mark a corrupt or mismatched generation aside;
- never start an implicit full rebuild from search or Context Pack;
- use lexical-only recall in `auto`, return a typed error in `required`, and
  provide the explicit rebuild command.

### Refresh Contention

- use bounded lock waits and short write transactions;
- continue from the last readable generation when safe;
- surface stale-index status rather than hanging;
- keep full rebuild as the correctness oracle.

### Process Crash

The next semantic request follows API-first recovery. It cleans stale state,
starts one matching daemon, waits for readiness, and retries once.

## Security And Privacy

- Bind inference endpoints to loopback only.
- Require a random API key from a current-user-only file for every request.
- Start `llama serve` with a local model path, embedding mode, CLS pooling, L2
  normalization, offline mode, disabled Web UI, disabled request-content
  logging, and the immutable bundle ID as its model alias; verify
  that alias and the embedding dimension before accepting output.
- Treat PID plus process start time as the process identity; never kill an
  unknown process merely because it occupies a cached port.
- Never send knowledge text, queries, embeddings, or logs to a cloud service.
- Treat the canonical immutable manifest embedded in the released Worktrail
  binary as the v1 runtime trust root. Before every daemon start and whenever
  an installed bundle, descriptor, or active generation is reused, revalidate
  its manifest identity; model, runtime, license, and attribution artifact
  types, sizes, and SHA-256 values; and the local chip variant against that
  manifest. Only a complete match may start or reuse semantic runtime.
- Any bundle, profile, generation, or daemon identity mismatch emits a visible
  warning and stable reason. It never starts or reuses the mismatched runtime:
  `auto` falls back to lexical retrieval, while `required` returns its stable
  error. These checks complement, rather than replace, ordinary Worktrail
  release tag and binary-distribution integrity.
- Record upstream model revision, license, and attribution.
- Do not execute downloaded shell installers.
- Do not add the managed `llama` executable to `PATH`.
- Do not allow the runtime to fetch a model or contact an external service;
  release validation observes the process under blocked outbound networking.
- Use atomic download and install paths.
- Restrict runtime descriptor and log permissions to the current user where the
  platform permits.
- Avoid logging query text, chunk bodies, vectors, or sensitive frontmatter.
- Treat model and runtime updates as supply-chain changes.
- Keep profile databases in existing gitignored local index paths.

## Observability

Diagnostics should report:

- active bundle and recall profile;
- semantic availability;
- daemon PID, endpoint, uptime, authenticated alias, and loaded bundle;
- cold start and warm request latency;
- tokenization and embedding batch latency;
- indexed entry and chunk counts;
- reused, added, changed, and deleted chunk counts;
- raw vector and database size;
- lexical, semantic, fused, and final candidate counts;
- upgrade, catch-up, activation, rebuild, and cleanup events;
- explicit degraded-mode reason codes.

Logs must prefer identifiers and timings over source content.

## Performance And Scale

The first release targets personal project and user knowledge stores. The
working scale assumption is up to approximately 100,000 chunks per scope, but
this is an evaluation target rather than a guaranteed limit.

Initial performance policy:

- exact KNN before adopting ANN complexity;
- batch document embeddings during rebuild;
- query embeddings through a warm shared daemon where available;
- no model initialization for non-semantic commands;
- incremental refresh based on source and chunk hashes;
- bounded candidate sets and neighbor expansion;
- measure cold and warm behavior separately.

If exact KNN misses the latency target at observed corpus sizes, evaluate stable
sqlite-vec indexing features or another local index in a separate architecture
decision. Do not adopt experimental ANN preemptively.

## Introduction And Existing Contract Preservation

The feature can be introduced without replacing the existing index:

1. ship bundle and runtime verification behind an explicit capability;
2. add structural chunk generation without changing existing entry search;
3. build profile-local chunk FTS and vectors;
4. expose hybrid retrieval through explicit `search --semantic`;
5. keep existing text output and JSON v1 stable while adding opt-in JSON v2;
6. integrate semantic selection only into Context Pack knowledge sections after
   quality and degradation contracts pass;
7. retain lexical-only operation and `--no-semantic`.

Existing project and user knowledge files need no format migration.

Semantic installation failure must not make `worktrail init`, `review`,
`maintain`, or other non-semantic workflows unusable.

Old semantic generations are not migrated. The active Worktrail binary either
recognizes the exact profile or rebuilds from Markdown and frontmatter.

## Validation Plan

### Runtime Parity

Use official FlagEmbedding output as the reference implementation and compare:

- Chinese text;
- English text;
- mixed Chinese and English;
- Go, Python, JavaScript, SQL, commands, and file paths;
- short and near-limit inputs;
- single and batch requests.

Validate vector shape, CLS pooling, L2 normalization, single/batch stability,
similarity ordering, and retrieval ranking. An exact acceptable tolerance must
be established before accepting the pinned BGE-M3 Q8_0 artifact and selecting
the first llama.app release candidate.

For every `verified` runtime variant proposed for inclusion in a release
manifest, validate on its matching physical Apple chip that the exact `llama`
executable supports:

- embedding server mode through `llama serve`;
- loopback-only binding;
- API-key authentication;
- a stable model alias;
- disabled Web UI and request-content logging;
- offline operation with a local GGUF path;
- CLS pooling and L2 normalization;
- tokenization, model-information, and embedding APIs.

For an `experimental` M2-M5 variant, pin its own compressed/final artifact
digests and require an installation-time local self-check of the same
artifact-integrity, loopback, API-key, alias, tokenization, embedding shape,
CLS-pooling, and L2-normalization contract. A failed self-check never attempts
another chip's binary; it visibly degrades to lexical behavior. Experimental
variants make no compatible or verified, performance, privacy, minimum-macOS,
or operational-support claim. They do not require pre-release per-chip
self-check reports.

### Chunking

Golden fixtures cover:

- nested headings;
- tiny and oversized sections;
- paragraphs and sentence-boundary splitting;
- numbered procedures and lists;
- fenced code;
- large tables with repeated headers;
- repeated headings;
- frontmatter and volatile metadata exclusion;
- deterministic IDs and neighbor links;
- incremental refresh matching full rebuild.

### Retrieval Quality

Build a Worktrail-specific labeled query set and compare:

- existing entry FTS;
- chunk FTS;
- dense semantic retrieval;
- fused lexical and semantic retrieval;
- fused retrieval plus governance reranking.

Measure at least Recall@10 and one rank-sensitive metric such as MRR or nDCG.
Include queries for exact identifiers, paraphrases, Chinese/English
cross-language lookup, decisions, workflows, rules, active state, and the latest
valid handoff.

### Runtime Lifecycle

Verify:

- the first semantic request starts the matching daemon;
- normal warm requests do not make a separate health call;
- concurrent agents start only one daemon per bundle;
- different bundles allocate non-conflicting endpoints under concurrency and
  recover from a lost bind race without killing the occupying process;
- stale PID and endpoint state recover;
- an incorrect API key, model alias, process start time, or embedding dimension
  is rejected;
- an unknown process on the cached port is never killed;
- 4xx and inference timeout do not cause restart loops;
- a crashed server restarts on the next request;
- non-semantic commands do not load the model.

### Init, Interface, And Context Pack

Verify:

- plain `worktrail init` does not prompt for semantic installation or access
  the network;
- `worktrail init --semantic` is the only semantic-installation entry point,
  shows download cost, and `worktrail init --no-semantic` explicitly disables
  installation;
- semantic installation failure leaves core initialization successful;
- installation verifies the pinned model's `634553760`-byte size and SHA-256;
- runtime installation verifies compressed and executable digests, performs
  pure-Go zstd decompression, and never executes a shell installer;
- canonical manifest fixtures produce stable content-addressed bundle IDs, and
  changing a trusted runtime or model field produces a different bundle path;
- interruption before the final directory rename never exposes a partial
  bundle as installed;
- the managed runtime stays in the Worktrail cache and does not modify `PATH`;
- successful installation does not build a generation and prints
  `worktrail semantic rebuild --scope all`;
- existing search text and JSON v1 fixtures remain unchanged;
- JSON v2 reports profile, lanes, chunk matches, and degraded reasons;
- active state, latest handoff, recovery, and maintenance remain present and
  deterministic when semantic similarity is low or unavailable;
- candidate, evidence, runtime, historical state, and old handoffs are excluded
  by the default indexing policy.

### Upgrade And Rebuild

Verify:

- download and disk requirements are shown before mutation;
- an interrupted build leaves the old profile active;
- source changes during a build are caught up before activation;
- atomic activation never mixes vector spaces;
- active generations are opened read-only, run no schema or repair SQL, and
  remain byte-for-byte unchanged across queries;
- every profile mismatch forces a full rebuild;
- unrecognized generation schemas are never opened;
- the replaced generation is removed after leases drain and is never used as a
  rollback target;
- post-activation failure degrades visibly to lexical-only;
- bundle GC is presented as reinstallable cache eviction;
- cross-scope profile mismatch uses lexical-only in `auto` mode and fails in
  `required` mode.

### Platform And Packaging

At minimum validate:

- every `verified` Apple M-series variant claimed by the release, using its
  exact pinned Metal runtime and minimum supported macOS version;
- every experimental M2-M5 variant uses only its own pinned artifact and passes
  its bounded install-time self-check; its failure degrades visibly to lexical
  behavior rather than reporting a verified capability. Pre-release per-chip
  self-check reports are not required;
- semantic operations return `semantic_platform_unsupported` on M1-M5 variants
  that are not listed in that release's trusted manifest;
- explicit `semantic_platform_unsupported` behavior on A18, Intel macOS,
  Linux, Windows, and other unsupported targets while lexical commands remain
  available;
- checksum verification and corrupt-download recovery;
- sqlite-vec registration in the shipped modernc SQLite build.

## Architecture Acceptance Criteria

The architecture direction is ready for implementation planning. Production
implementation after the bounded validation spikes may proceed only when:

- the pinned BGE-M3 Q8_0 GGUF passes FlagEmbedding dense parity;
- one llama.app version and M1, the sole `verified` runtime variant, pass the
  physical-hardware runtime contract and are recorded with immutable URLs,
  compressed/final sizes, SHA-256 values, and a minimum macOS version; every
  experimental M2-M5 variant has its own immutable artifact fields and bounded
  local self-check, without compatible or verified, performance, privacy,
  minimum-macOS, or operational-support claims;
- trusted bundle installation and cache/runtime/log ownership are specified;
- process supervision behavior is defined for supported operating systems;
- runtime identity is authenticated and model identity is verified;
- chunk fixtures establish deterministic behavior;
- generation schema and atomic activation are prototyped;
- profile mismatch demonstrably causes rebuild rather than migration or reuse;
- build-time source changes are caught up before activation;
- hybrid retrieval beats or preserves the lexical baseline on the labeled set;
- governance ordering remains effective after RRF;
- Context Pack deterministic sections remain unchanged;
- existing search text and JSON v1 contracts remain unchanged;
- plain `worktrail init` remains offline by default, and `--no-semantic`
  explicitly disables semantic installation;
- normal non-semantic startup is unaffected;
- failure and degradation are machine-readable and visible;
- no source knowledge or query leaves the machine.

## Risks

### Model And Runtime Footprint

BGE-M3 Q8_0 is 634,553,760 bytes before runtime and temporary decompression
overhead. A warm daemon permanently consumes meaningful memory. Installation
must make model/runtime download size, peak disk use, and final cache location
visible.

### llama.app BGE-M3 Compatibility

BGE-M3 has had model-specific pooling, tokenization, batching, and endpoint
compatibility issues across llama.cpp builds. The unified llama.app program is
also evolving. Worktrail must pin and test one known-good llama.app version and
every M-series variant advertised by that release rather than resolve or assume
current `latest` behavior during installation.

### Background Process Complexity

Detachment, stale-process cleanup, locking, logs, upgrades, and authenticated
identity add operational behavior that did not exist in the lexical-only
boundary. Restricting v1 to macOS M-series reduces, but does not remove, this
complexity.

### sqlite-vec Pre-v1 Evolution

sqlite-vec remains pre-v1 and may introduce storage or SQL changes. Isolating
generations, pinning the bundled version, and rebuilding on every profile change
avoid migration complexity but may impose a full re-embedding cost.

### Rebuild Cost And No Generation Rollback

Strict rebuilds temporarily duplicate model and index storage. The first release
does not retain old generations or provide a rollback command. Failures after
activation fall back to lexical recall until a corrected or previously selected
profile is rebuilt from Markdown.

### Retrieval Quality

General embedding benchmarks do not prove quality on Worktrail knowledge.
Selection and tuning must be driven by a project-specific labeled corpus.

## Residual Assumptions

- **Assumption:** a user-level model cache is acceptable because an immutable
  bundle can be shared by multiple projects.
  **Validation method:** run install, concurrent-use, and cache-eviction tests
  across at least two project roots.
- **Assumption:** exact KNN is sufficient through the initial target scale.
  **Validation method:** benchmark 10,000, 50,000, and 100,000 chunks on at
  least one lower-bound and one current Apple M-series target, recording
  database size and cold/warm P50/P95.
- **Assumption:** 512 target tokens and 768 hard-limit tokens are a reasonable
  starting point.
  **Validation method:** compare retrieval quality and citation coherence over
  multiple token-budget configurations on the labeled Worktrail corpus.
- **Assumption:** the chosen llama.app/BGE-M3 Q8_0 output is sufficiently
  faithful to the original BGE-M3 dense output for Worktrail retrieval.
  **Validation method:** compare dimensions, normalization, batch stability,
  top-K overlap, Recall@10, MRR, and nDCG with FlagEmbedding over Chinese,
  English, mixed-language, and code queries.
- **Assumption:** Worktrail can detect a release-supported M1-M5 variant
  reliably and run its pinned official artifact without invoking the upstream
  installer.
  **Validation method:** a `verified` family passes the exact artifact on
  matching physical hardware, including `llama version`, minimum-macOS behavior,
  API capabilities, and blocked-outbound operation. An `experimental` M2-M5
  family passes only its own bounded local self-check and makes no compatible or
  verified, performance, privacy, minimum-macOS, or operational-support claim.

## Implementation Parameters To Resolve By Validation

- exact pinned llama.app version; the physically validated M1 subset; and the
  experimental M2-M5 subset, each with runtime URLs, compressed/final sizes,
  and SHA-256 values;
- idle timeout and maximum number of warm candidate bundles;
- top-K depths, RRF constants, diversity limits, and quality thresholds;
- release evidence for crossing the original no-daemon/no-vector boundary in
  v1.0.0.

## Decision Register

The following decisions are accepted for this architecture proposal and should
be extracted into ADR artifacts before implementation:

- Use the pinned `ggml-org/bge-m3-Q8_0-GGUF` artifact for BGE-M3 dense
  1024-dimensional embeddings in the first profile, subject to FlagEmbedding
  parity.
- Use a release-pinned official llama.app `llama` runtime rather than requiring
  Ollama, Python, CMake, or a llama.cpp source build.
- Limit semantic runtime candidates in v1 to macOS Apple M1-M5. Classify
  M1 as `verified`; permit M2-M5 `experimental` variants with their own pinned
  artifact and bounded local self-check, without compatible or verified,
  performance, privacy, minimum-macOS, or operational-support claims.
- Decode official `.zst` runtime artifacts in-process with a pinned pure-Go
  decoder; never execute the upstream installer or an `unzstd` helper.
- Let Worktrail install immutable semantic bundles only through
  `worktrail init --semantic`.
- Manage a loopback user-level daemon per required bundle with API-first
  recovery.
- Use profile-local sqlite-vec generation databases and exact cosine KNN.
- Use structural Markdown chunking with deterministic token-bounded output.
- Fuse chunk FTS and vector ranks with RRF before governance reranking.
- Preserve existing Worktrail contracts while treating semantic generations as
  rebuild-only derived data.
- Upgrade through side-by-side rebuild, source catch-up, validation, and atomic
  activation, without semantic schema migration, generation retention, or
  rollback.
- Restrict semantic Context Pack influence to knowledge sections.

## Recommended Implementation Order

1. Validate BGE-M3 Q8_0 against FlagEmbedding and validate a candidate
   llama.app version on each Apple chip family intended for the first release;
   publish support only for the variants that pass.
2. Define the trusted immutable manifest, Apple-chip selection, pure-Go zstd
   extraction, and explicit `--semantic` / `--no-semantic` installer boundary.
3. Implement authenticated runtime identity and concurrency-safe API-first
   recovery.
4. Add the versioned indexing policy and structural chunk fixtures.
5. Prototype the profile-local generation database and strict profile identity.
6. Implement full rebuild, source catch-up, validation, and atomic activation.
7. Implement hybrid retrieval, explicit degradation, and stable v1/v2 outputs.
8. Integrate semantic selection into Context Pack knowledge sections only.
9. Add cache cleanup and rebuild recovery without generation retention,
   rollback, or semantic migration.
10. Run the complete platform, performance, and Worktrail-specific retrieval
    evaluation for M1. For experimental M2-M5 variants, preserve the
    installation-time self-check gate and collect later target-hardware evidence
    for any future support-tier change.
11. Promote the runtime/model ADR and proceed beyond validation spikes only
    after the acceptance criteria above identify the first supported bundle and
    release boundary.

## Related Documents

- [Worktrail SQLite + GSE Index Design](worktrail-sqlite-gse-index-design.md)
- [Worktrail Long-Term Vision Discussion](worktrail-long-term-vision-discussion.md)
- [Knowledge Layering And Candidate Boundaries](knowledge-layering-and-candidate-boundaries.md)
- [Semantic Text Safety Contract](semantic-text-safety-contract.md)
- [Review Source Traceability](review-source-traceability.md)
