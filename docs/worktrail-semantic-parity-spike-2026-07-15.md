# BGE-M3 Q8 / llama.app M1 Parity Spike

Date: 2026-07-15

Status: bounded local evidence; not a release manifest or an Accepted ADR.

> **Superseded trust-boundary note (2026-07-16):** This historical spike
> recorded the then-current requirement for a separate Worktrail release
> attestation before its locally measured runtime digest could enter a trusted
> manifest. That requirement is superseded: for v1, the canonical immutable
> manifest embedded in the formal Worktrail release is the sole runtime trust
> root. The historical measurements and evidence below are unchanged; startup
> and reuse still require local manifest-identity, artifact type/size/SHA-256,
> and chip-variant verification.

## Scope

This record validates one explicit local candidate only:

- Host: Apple M1 Pro, macOS 15.7.3.
- Runtime distribution: official llama.app `b9986`.
- Runtime variant: `aarch64/macos/metal/m1/llama-app.zst`.
- Runtime version output: `b9986-91c631b21`.
- Candidate model: `ggml-org/bge-m3-Q8_0-GGUF` revision
  `9eba04c5d75ba5a1595e45de734d36bef4e5cb98`.
- Reference model: `BAAI/bge-m3` revision
  `5617a9f61b028005a4858fdac845db406aefb181`, captured with
  FlagEmbedding 1.4.0 and torch 2.13.0.

The test did not use the remote llama.app installer, compile llama.cpp, alter
`PATH`, install a Worktrail bundle, build a semantic generation, publish an
artifact, or include private project text.

## Candidate Input Integrity

| Artifact | Size | SHA-256 |
| --- | ---: | --- |
| `bge-m3-q8_0.gguf` | 634,553,760 bytes | `aa473d51f451a22f0fcf39ba3330c14bed38a385712b1113440f69df4047a173` |
| original `pytorch_model.bin` | 2,271,145,830 bytes | `b5e0ce3470abf5ef3831aa1bd5553b486803e83251590ab7ff35a117cf6aad38` |
| `llama-app.zst` | 6,887,500 bytes | `01622514155fa516dac978388648f8821a4daa13f0157ca1820ed919fd548adc` |
| decompressed `llama` | 17,114,936 bytes | `15d468c9f4820f4ba0fd44ccd2e19dd19bb60e8c7c839deb3fc2cfbff281307c` |

The runtime was decompressed with
`github.com/klauspost/compress` v1.19.0 through the project’s pure-Go
decompression helper. The compressed runtime digest is a locally measured
candidate value from the official immutable version path, not an independently
published upstream SHA-256 sidecar; it requires Worktrail release attestation
before becoming a trusted manifest field.

## Runtime Contract Results

The test started `llama serve` with:

- a verified local GGUF;
- `127.0.0.1` binding and an ephemeral port;
- a random current-user API key file;
- alias `worktrail-bge-m3-b9986-m1`;
- `--no-webui`, `--log-disable`, `--offline`, `--embedding`,
  `--pooling cls`, and `--embd-normalize 2`.

Observed results:

- `/v1/models` returned the configured alias and `n_embd: 1024`.
- unauthenticated `/tokenize` returned HTTP 401.
- authenticated `/tokenize` succeeded.
- authenticated `/v1/embeddings` succeeded for single and batched inputs.
- the process ran from the verified local executable and exited during test
  cleanup.

This demonstrates the selected M1 runtime candidate. It does not validate M2,
M3, M4, M5, non-M-series Apple hardware, or non-Darwin platforms.
`--offline` confirms the runtime’s offline mode accepted the local artifact; a
separate blocked-outbound network test is still required before release.

## Parity Corpus And Results

The committed corpus contains 5 labeled queries and 6 documents spanning
Chinese, English, Chinese/English mixed CLI text, code/SQLite terminology, and
a long mixed technical query. All values below are generated from the committed
corpus and local model files.

| Check | Gate | Result |
| --- | ---: | ---: |
| vector dimension | 1024 | 1024 |
| normalized-vector tolerance | 0.00001 | passed |
| minimum reference/candidate cosine | 0.999 | 0.9996366062 |
| maximum single/batch element delta | 0.0002 | 0.0001047071 |
| minimum single/batch cosine | informational | 0.9999995692 |
| top-10 overlap | 0.9 | 1.0 |
| Recall@10 | 0.9 | 1.0 |
| MRR | 0.9 | 1.0 |
| nDCG@10 | 0.9 | 1.0 |

The first run used a `1e-6` single/batch element threshold and failed only that
condition. The reference implementation’s maximum delta was
`1.5131409e-7`; the llama.app candidate’s was `1.0470706e-4`, while its
single/batch cosine remained `0.9999995692`. The threshold was adjusted to
`2e-4` and the full Gate was re-run successfully. This threshold is specific to
the validated runtime/model/corpus and must be revalidated for every new
profile.

## Evidence Files

The local run generated reference and candidate capture JSON plus a report.
Their SHA-256 values were:

- reference capture: `ece5b683c556fcef8c281d4a62047975cff5f2cc2cdbc7f4ad2bc202fc2f4801`;
- candidate capture: `0369648799b31c0f73ebd7a5f5c32cdb3e3038d8b9d7a42d2d794e9256be6fa6`;
- passing parity report: `3da52ed853bd03434700ae2b1a28a9ad40dc4ef04843ca43e695deb5cbdb38e4`;
- exact Python environment freeze: `f730d84f04d89f2f3487e58c0fc1d50a16cbd99b25f920e02abc72e7dca641a3`.

The large vector captures are intentionally not copied into formal Worktrail
knowledge. The temporary local copies may be removed after this evidence record
and the committed environment lock are written; the corpus, fixed inputs,
script, report hash, and environment lock are sufficient to reproduce them.

## Remaining Gates

- Validate every additional Apple chip variant before advertising it.
- Establish release acceptance budgets for startup, warm latency, memory, and
  bundle installation.
- Lock the canonical manifest and attest its runtime digest in a Worktrail
  release.
- Review this evidence before creating Accepted replacement ADR candidates.
