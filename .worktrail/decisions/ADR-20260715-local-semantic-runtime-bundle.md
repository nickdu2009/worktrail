---worktrail
{
  "created_at": "2026-07-17T02:52:11.191485Z",
  "id": "ADR-20260715-local-semantic-runtime-bundle",
  "lifecycle": "current",
  "schema": "worktrail.knowledge.v1",
  "scope": "project",
  "stage": "decision",
  "status": "accepted",
  "title": "Use a Worktrail-Managed llama.app BGE-M3 Q8 Bundle",
  "type": "decision",
  "updated_at": "2026-07-21T02:43:50.4689Z"
}
---

# ADR-20260715-local-semantic-runtime-bundle: Use a Worktrail-Managed llama.app BGE-M3 Q8 Bundle

- Status: Accepted
- Date: 2026-07-17

## Context

Worktrail v1 local semantic recall has one physically verified M1 runtime. Users may run M2-M5 machines before the project has per-chip pre-release runtime evidence. Shipping those variants as compatible would overstate their support level and conflict with the release-evidence policy.

## Decision

Use a Worktrail-managed, content-addressed semantic bundle containing the pinned BGE-M3 Q8_0 GGUF, one release-pinned llama.app version, classified Apple runtime variants, the canonical trusted manifest, licenses, and attribution. The bundle ID is the SHA-256 of the versioned canonical manifest excluding its own ID, signatures, timestamps, and mutable local state.

M1 remains the only `verified` variant. It retains the complete physical-hardware gate, minimum-macOS declaration, and resource budget.

M2, M3, M4, and M5 ship only as opt-in `experimental` variants. Every variant must select its own immutable official llama.app artifact pin and must pass the local installation-time integrity, authenticated-loopback, alias, tokenization, embedding-dimension, CLS-pooling configuration, and L2-normalization self-check before activation. A variant must never fall back to another chip artifact.

Experimental variants make no compatible or verified performance, privacy, minimum-macOS, or operational-support claim. A failed local self-check rejects the bundle and visibly degrades semantic auto mode to lexical behavior with the existing stable semantic reason codes. Pre-release per-chip self-check reports are not a release gate for experimental variants. User feedback and later target-hardware evidence may promote a variant through a future ADR update.

Core init remains network-free and semantic installation may be triggered only by the explicit `worktrail init --semantic` flag. `worktrail init --no-semantic` explicitly disables semantic installation. Worktrail downloads only immutable manifest URLs, verifies compressed and final sizes and SHA-256 values, decompresses llama-app.zst in process with the pinned pure-Go zstd package, stages the complete bundle, and atomically renames the verified directory. It never executes a remote installer, compiles llama.cpp, or changes PATH.

Semantic operations may start llama serve as a detached current-user process. It binds only to authenticated loopback, uses the bundle ID as alias, loads only the verified local GGUF, disables unwanted UI, request-content logging, metrics, and network behavior, and uses API-first recovery. Worktrail verifies runtime/model files before launch and verifies API alias, dimension, and response contracts without assuming the API exposes file SHA-256. Unknown processes are never killed.

## Consequences

### Positive

- M2-M5 users can opt into pinned, locally checked runtime artifacts without a false production-support claim.
- M1 verification and its safety/performance envelope remain unchanged.
- Artifact integrity, loopback authentication, and safe lexical degradation remain mandatory on every chip.

### Negative

- M2-M5 users receive an explicit experimental warning and no performance or compatibility guarantee.
- Runtime support levels, status output, release acceptance, and documentation must distinguish experimental from verified.
- A manifest-level tier change creates a new bundle ID and requires reinstall/rebuild.

## Links

- Related: architecture/local-semantic-recall.md
- Related: requirements/release-acceptance.md
- Related: validation/release-validation-checklist.md
- Evidence: evidence/semantic-m1/release-evidence-2026-07-17.md
- Evidence: evidence/semantic-m1/production-e2e-2026-07-17.md
- Historical evidence provenance: parity and runtime/security spikes dated 2026-07-15 and 2026-07-16 at `docs/archive/semantic-recall-m1-prep-2026-07/spikes/`; after source consolidation, the records remain recoverable from Git history.
