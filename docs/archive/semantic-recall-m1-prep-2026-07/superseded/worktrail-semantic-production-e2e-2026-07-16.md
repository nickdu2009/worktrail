# Worktrail Semantic Production E2E Evidence

- platform_scope: verified M1 only; M2-M5 are experimental and out of scope
- git_revision: 91907b1a7a6bdce0f8fd6f3ca3c3d91205f8eac4
- macos: 15.7.3
- machine: Apple M1 Pro
- finished_at: 2026-07-16T11:07:33Z
- result: PASS

## Command versions

- worktrail: built from repo HEAD
- worktrail-semantic-eval: built from repo HEAD

## Retrieval report summary

```
passed True
entry_fts recall@10 0.8889 mrr 0.8889 ndcg@10 0.8889 vs_entry
chunk_fts recall@10 0.8889 mrr 0.8889 ndcg@10 0.8889 vs_entry
dense recall@10 1 mrr 0.4074 ndcg@10 0.5556 vs_entry
rrf recall@10 1 mrr 0.9259 ndcg@10 0.9444 vs_entry ge
governed recall@10 1 mrr 0.6759 ndcg@10 0.7547 vs_entry ge
```

## Resource summary

```
{
  "schema": "worktrail.semantic.eval.runtime-resource.v1",
  "runtime": {
    "version": "b9986-91c631b21",
    "alias": "worktrail-bge-m3-b9986-m1",
    "endpoint": "http://127.0.0.1:60628",
    "model_dimension": 1024
  },
  "network_policy": {
    "sandbox_profile": "allow default; deny network-outbound",
    "api_succeeded_under_policy": true,
    "unauthenticated_tokenize_status": 401
  },
  "cold_start_ms": 1085.1735,
  "warm_embedding_samples": 20,
  "warm_embedding_p50_ms": 10.4869995,
  "warm_embedding_p95_ms": 11.148958,
  "peak_rss_kb": 819584
}
```

## Residual risk

- This M1 gate does not provide M2-M5 hardware, performance, privacy,
  minimum-macOS, or operational-support evidence. That absence is not a release
  blocker for their experimental tier: each M2-M5 installation remains gated by
  its own pinned artifact and local installation-time self-check, with no
  cross-chip fallback. It must not be presented as compatible or verified.
- Evidence artifacts under temporary root are deleted on exit unless WORKTRAIL_E2E_KEEP_TMP=1.
