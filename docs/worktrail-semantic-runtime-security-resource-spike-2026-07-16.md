# llama.app M1 Security And Resource Spike

Date: 2026-07-16

Status: bounded local evidence; not a trusted install manifest or release SLO.

> **Superseded trust-boundary note (2026-07-16):** The later text in this
> historical spike refers to a separate release attestation. That former
> requirement is superseded: the canonical immutable manifest embedded in the
> formal Worktrail release is the sole v1 runtime trust root. The evidence and
> measurements below remain historical facts; startup and reuse still require
> local manifest-identity, artifact type/size/SHA-256, and chip-variant
> verification.

## Scope

The test used the same fixed local M1 candidate as the parity spike:

- Host: Apple M1 Pro, macOS 15.7.3.
- Runtime: llama.app `b9986-91c631b21`.
- Runtime artifact: `aarch64/macos/metal/m1/llama-app.zst`.
- Model: BGE-M3 Q8_0 revision
  `9eba04c5d75ba5a1595e45de734d36bef4e5cb98`.

The runtime executable and GGUF were downloaded through their immutable official
version/revision URLs and verified before launch. The test did not install a
Worktrail bundle, change `PATH`, create a generation, or publish an artifact.

## Outbound-Network Gate

`llama serve` ran through the macOS `sandbox-exec` profile:

```text
(version 1)
(allow default)
(deny network-outbound)
```

Under that policy, the runtime:

- started from the local verified model;
- bound only to `127.0.0.1`;
- returned HTTP 401 for unauthenticated `/tokenize`;
- returned a 1024-dimensional authenticated embedding response;
- served 20 authenticated warm embedding requests.

`lsof` observed only the loopback listener. The result establishes that the
candidate API works when the child process has no outbound network permission.
It is not a substitute for later installer, process-recovery, multi-user, or
kernel-firewall validation.

## Resource Measurements

| Measure | Result |
| --- | ---: |
| cold start to authenticated `/v1/models` readiness | 13.840 s |
| warm embedding samples | 20 |
| warm embedding P50 | 11.992 ms |
| warm embedding P95 | 15.771 ms |
| peak runtime RSS | 760,032 KiB (about 742 MiB) |

These are one-host, no-concurrency measurements rather than accepted release
budgets. The final release must define a supported-hardware resource envelope,
startup and warm-latency limits, and a measurement procedure before treating
them as acceptance criteria.

## Minimum macOS Evidence

`otool -l` reports Mach-O `LC_BUILD_VERSION minos 13.3` for this exact
executable. That is binary metadata, not physical validation of runtime
behavior on macOS 13.3. The only physical evidence remains macOS 15.7.3, so a
release manifest must not yet claim 13.3 as its verified minimum.

## License And Attribution Sources

- BGE-M3 Q8_0 model page: MIT tag; repository `ggml-org/bge-m3-Q8_0-GGUF`,
  revision `9eba04c5d75ba5a1595e45de734d36bef4e5cb98`, based on `BAAI/bge-m3`.
- Runtime source: `ggml-org/llama.cpp` MIT License, GitHub license blob
  `e7dca554bcb802f98408383a864404e3aa4eacca`.
- Distribution provenance: official llama.app bucket version `b9986`.

The final trusted manifest still needs to embed the applicable license texts
and attribution files with the selected artifacts; recording source licenses
does not itself grant a release attestation.

## Evidence Hashes

- runtime/resource report:
  `174a31fe830ea94b1658fc827ec202e6bb2c9a08df491c7b4186d29dc6399db3`;
- loopback socket observation:
  `5088d00e672cd6a819fae3a828c80d84319be910beaf4a992e9007f8d752b88d`;
- sandbox profile:
  `e210de323e5d671bb6b63b79ce1481f8314b5ac2fd65d65a180d381294ee2fa1`;
- executable Mach-O inspection:
  `6bf9cb43806ef45ff063b0bdb29cc45dae6fe3300056dce659b214a1fdeef96b`.
