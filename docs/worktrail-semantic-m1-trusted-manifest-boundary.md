# M1–M5 canonical embedded-manifest runtime trust boundary

## Embedded input

`internal/semantic/bundle/assets/trusted-manifest-m1.json` is a pinned,
multi-variant input to the bundle package. Its canonical bundle ID is
`3625d27700727578d7694ab04d19291efce45095aa57daba66d692a37a51be58`.
It records the locally verified M1 envelope:

- Apple M1 Pro on macOS 15.7.3;
- llama.app `b9986`, executable `b9986-91c631b21`;
- BGE-M3 Q8_0 GGUF revision `9eba04c5d75ba5a1595e45de734d36bef4e5cb98`;
- verified runtime resource-budget limits from the bounded M1 measurement.

The manifest references embedded MIT text and attributions by path and SHA-256.
It additionally pins one independently measured official llama.app `b9986`
artifact for each of M2, M3, M4, and M5. Those variants are opt-in
`experimental`, without compatible or verified, performance, privacy,
minimum-macOS, or operational-support claims.

## Experimental M2–M5 installation boundary

M2, M3, M4, and M5 each select only their own embedded artifact pin. Installation,
reuse, loading, and runtime verification require the current chip's executable
and model, not every listed executable; they never substitute or fall back to
the M1 artifact. An experimental install must pass the authenticated loopback
daemon self-check (alias, tokenization, 1024-dimensional L2-normalized
embedding) before it can proceed. Failure removes local enabled state and
the bundle so it cannot be composed or started later when the self-check
created the daemon. A repeated installation that passes the complete self-check
retains its existing descriptor, key, and process; a token or embedding failure
after authenticated reuse stops and removes that confirmed daemon before bundle
rejection, so Worktrail never leaves an unmanaged process. Installation and
self-check failures surface the stable `semantic_runtime_unavailable` reason
and leave lexical recall available. If that controlled stop or state cleanup
fails, Worktrail retains the integrity-verified bundle, descriptor, and key and
still returns the stable reason, so a later controlled stop or recovery remains
possible. Store cleanup restores the descriptor if API-key deletion fails,
preventing partial daemon state.

`experimental` carries only a pinned artifact and the installation self-check
gate. It does not claim `compatible` or `verified`, inherit M1's macOS 15.7.3
minimum or resource budget, promise privacy or operational support, or require
a pre-release per-chip self-check report. Later target-hardware evidence may
support a future ADR decision to change its tier.

A18 and non-Darwin remain unsupported, rather than experimental candidates.

## Runtime trust boundary

When this canonical immutable manifest is embedded in a formal Worktrail
release, it is the sole runtime trust root for the table-aware profile
(`generation_schema_version: 2`, `chunker-v2`, `worktrail-fts5-gse-v2`). No
independent release-attestation, signature, or verifier is required to authorize
startup or production installation.

Before startup and runtime reuse, Worktrail must revalidate the installed
bundle's manifest identity; model, selected current-chip runtime, license, and
attribution artifact types, sizes, and SHA-256 values. A complete
match authorizes semantic runtime; every bundle, profile, generation, or daemon
identity mismatch must surface a warning and stable reason, degrade `auto` to
lexical, and fail `required` stably. Missing or old bundles point operators to
`worktrail init --semantic`; a current bundle with a v1 generation points to a
scoped `worktrail semantic rebuild`.

M1 remains the only bounded `verified` input. M2–M5 use production installer
integration and local self-checks as `experimental` variants. Normal Worktrail
tag and binary-distribution integrity continues to apply.

## Public attribution sources

- Converted model: https://huggingface.co/ggml-org/bge-m3-Q8_0-GGUF/tree/9eba04c5d75ba5a1595e45de734d36bef4e5cb98
- Original model: https://huggingface.co/BAAI/bge-m3/tree/5617a9f61b028005a4858fdac845db406aefb181
- M1 runtime distribution: https://huggingface.co/buckets/ggml-org/install.sh/resolve/b9986/aarch64/macos/metal/m1/llama-app.zst
- M2 runtime distribution: https://huggingface.co/buckets/ggml-org/install.sh/resolve/b9986/aarch64/macos/metal/m2/llama-app.zst
- M3 runtime distribution: https://huggingface.co/buckets/ggml-org/install.sh/resolve/b9986/aarch64/macos/metal/m3/llama-app.zst
- M4 runtime distribution: https://huggingface.co/buckets/ggml-org/install.sh/resolve/b9986/aarch64/macos/metal/m4/llama-app.zst
- M5 runtime distribution: https://huggingface.co/buckets/ggml-org/install.sh/resolve/b9986/aarch64/macos/metal/m5/llama-app.zst
- llama.cpp MIT license: https://github.com/ggml-org/llama.cpp/blob/master/LICENSE
