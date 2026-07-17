# sqlite-vec Exact Cosine Capacity Spike

Date: 2026-07-15

Status: bounded local evidence; not a production performance SLO.

## Method

The project’s `worktrail-semantic-eval vec` command generated deterministic
L2-normalized synthetic float32 vectors and inserted them into a fresh local
SQLite database using:

```sql
CREATE VIRTUAL TABLE vectors USING vec0(
  embedding FLOAT[1024] distance_metric=cosine
)
```

Each size ran 20 exact cosine KNN queries with `LIMIT 10`. The evaluator asserts
that sqlite-vec returns exactly the requested number of rows and that distances
are nondecreasing. It uses the repository-pinned `modernc.org/sqlite` v1.51.0
with sqlite-vec v0.1.9.

Host: Apple M1 Pro, macOS 15.7.3.

## Results

| Vectors | Database bytes | Insert time | Query P50 | Query P95 |
| ---: | ---: | ---: | ---: | ---: |
| 10,000 | 42,246,144 | 1.954 s | 18.976 ms | 19.578 ms |
| 50,000 | 206,893,056 | 9.920 s | 91.110 ms | 92.429 ms |
| 100,000 | 413,761,536 | 20.568 s | 186.535 ms | 188.943 ms |

The three JSON report SHA-256 values are:

- 10,000: `2e1e417fad0ca16376242fe4d6f703d0ec161dd17665c3f1154c30ebeae5be79`;
- 50,000: `92e36d3a23396279b4d715a97803b9c667b84283950896cc808776a619d23324`;
- 100,000: `ba33d4a367396cc87b6b6044d534bd1d453b893db3d4e5e8413d4300b9114c78`.

## Interpretation

The 100,000-vector database is close to the architecture’s raw-vector estimate
plus expected sqlite-vec overhead. Exact KNN is functional at the initial
working-scale assumption on this M1 candidate, but these results are not a
release guarantee:

- vectors are synthetic rather than BGE-M3 output;
- the report covers warm in-process queries only, not daemon startup or cold
  file-system behavior;
- it does not measure concurrent readers, active-generation leases, chunk FTS,
  hybrid fusion, or Context Pack assembly;
- the chip is M1 Pro only.

Release acceptance must still record at least a lower-bound and a current Apple
target, explicit latency/memory budgets, and the full end-to-end retrieval
path.
