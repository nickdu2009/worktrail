#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
gate_root="${1:?usage: run-runtime-security-resource-gate.sh <gate-root>}"
cd "$repo_root"

llama="$gate_root/llama"
model="$gate_root/bge-m3-q8_0.gguf"
key_file="$gate_root/api-key"
profile_file="$gate_root/deny-outbound.sb"
log_file="$gate_root/llama.log"
report_file="$gate_root/runtime-resource-report.json"
socket_file="$gate_root/runtime-sockets.txt"

for file in "$llama" "$model"; do
  test -e "$file" || { printf 'missing required gate input: %s\n' "$file" >&2; exit 1; }
done
test "$("$llama" version)" = "b9986-91c631b21"
test "$(shasum -a 256 "$llama" | awk '{print $1}')" = "15d468c9f4820f4ba0fd44ccd2e19dd19bb60e8c7c839deb3fc2cfbff281307c"
test "$(shasum -a 256 "$model" | awk '{print $1}')" = "aa473d51f451a22f0fcf39ba3330c14bed38a385712b1113440f69df4047a173"

port="$(python3 - <<'PY'
import socket
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)"
endpoint="http://127.0.0.1:$port"
umask 077
python3 - <<'PY' >"$key_file"
import secrets
print(secrets.token_urlsafe(32))
PY
printf '%s\n' \
  '(version 1)' \
  '(allow default)' \
  '(deny network-outbound)' \
  >"$profile_file"

start_ns="$(python3 - <<'PY'
import time
print(time.monotonic_ns())
PY
)"
sandbox-exec -f "$profile_file" "$llama" serve \
  --model "$model" \
  --host 127.0.0.1 \
  --port "$port" \
  --api-key-file "$key_file" \
  --alias worktrail-bge-m3-b9986-m1 \
  --no-webui \
  --log-disable \
  --offline \
  --embedding \
  --pooling cls \
  --embd-normalize 2 \
  >"$log_file" 2>&1 &
llama_pid=$!
cleanup() {
  kill "$llama_pid" 2>/dev/null || true
  wait "$llama_pid" 2>/dev/null || true
}
trap cleanup EXIT

endpoint="$endpoint" key_file="$key_file" start_ns="$start_ns" llama_pid="$llama_pid" report_file="$report_file" python3 - <<'PY'
import json
import os
import statistics
import subprocess
import time
import urllib.request
from urllib.error import HTTPError

endpoint = os.environ["endpoint"]
key = open(os.environ["key_file"], encoding="utf-8").read().strip()
start_ns = int(os.environ["start_ns"])
pid = os.environ["llama_pid"]

def request(path, payload=None, authenticated=True):
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    headers = {"Content-Type": "application/json"} if data else {}
    if authenticated:
        headers["Authorization"] = f"Bearer {key}"
    req = urllib.request.Request(endpoint + path, data=data, headers=headers, method="GET" if data is None else "POST")
    with urllib.request.urlopen(req, timeout=30) as response:
        return response.status, json.load(response)

deadline = time.monotonic() + 120
while True:
    try:
        status, models = request("/v1/models")
        if status == 200:
            break
    except Exception:
        if time.monotonic() >= deadline:
            raise
        time.sleep(0.25)
cold_start_ms = (time.monotonic_ns() - start_ns) / 1_000_000

try:
    request("/tokenize", {"content": "authentication gate"}, authenticated=False)
    raise RuntimeError("unauthenticated tokenize unexpectedly succeeded")
except HTTPError as error:
    if error.code != 401:
        raise

latencies = []
rss_kb = []
for _ in range(20):
    began = time.monotonic_ns()
    status, response = request(
        "/v1/embeddings",
        {"model": "worktrail-bge-m3-b9986-m1", "input": ["warm resource gate"]},
    )
    if status != 200 or len(response.get("data", [])) != 1 or len(response["data"][0]["embedding"]) != 1024:
        raise RuntimeError("embedding response contract mismatch")
    latencies.append((time.monotonic_ns() - began) / 1_000_000)
    rss = subprocess.check_output(["/bin/ps", "-o", "rss=", "-p", pid], text=True).strip()
    rss_kb.append(int(rss))

def percentile(values, fraction):
    values = sorted(values)
    index = min(len(values) - 1, max(0, round((len(values) - 1) * fraction)))
    return values[index]

report = {
    "schema": "worktrail.semantic.eval.runtime-resource.v1",
    "runtime": {
        "version": "b9986-91c631b21",
        "alias": "worktrail-bge-m3-b9986-m1",
        "endpoint": endpoint,
        "model_dimension": models["data"][0]["meta"]["n_embd"],
    },
    "network_policy": {
        "sandbox_profile": "allow default; deny network-outbound",
        "api_succeeded_under_policy": True,
        "unauthenticated_tokenize_status": 401,
    },
    "cold_start_ms": cold_start_ms,
    "warm_embedding_samples": len(latencies),
    "warm_embedding_p50_ms": statistics.median(latencies),
    "warm_embedding_p95_ms": percentile(latencies, 0.95),
    "peak_rss_kb": max(rss_kb),
}
with open(os.environ["report_file"], "w", encoding="utf-8") as output:
    json.dump(report, output, indent=2)
    output.write("\n")
PY

/usr/sbin/lsof -nP -a -p "$llama_pid" -iTCP >"$socket_file"
python3 - "$socket_file" <<'PY'
import pathlib
import sys

lines = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8").splitlines()[1:]
if not lines:
    raise SystemExit("llama has no listening TCP socket")
if any("127.0.0.1:" not in line and "[::1]:" not in line for line in lines):
    raise SystemExit("llama exposed a non-loopback TCP socket")
PY

otool -l "$llama" >"$gate_root/llama-otool.txt"
awk '/LC_BUILD_VERSION/{seen=1; next} seen && /minos/{print $2; exit}' "$gate_root/llama-otool.txt" >"$gate_root/llama-minimum-macos.txt"
shasum -a 256 "$report_file" "$socket_file" "$profile_file" "$gate_root/llama-otool.txt" >"$gate_root/hashes.txt"
