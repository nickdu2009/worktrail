#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
gate_root="${1:?usage: run-real-darwin-m1-gate.sh <gate-root>}"
cd "$repo_root"
python="$gate_root/venv/bin/python"
llama="$gate_root/llama"
model="$gate_root/bge-m3-q8_0.gguf"
reference_model="$gate_root/reference-model"
cases="$repo_root/testdata/semantic/bge-m3-parity-cases.json"
key_file="$gate_root/api-key"
log_file="$gate_root/llama.log"
endpoint_file="$gate_root/endpoint"

for file in "$python" "$llama" "$model" "$reference_model/pytorch_model.bin" "$cases"; do
  test -e "$file" || { printf 'missing required gate input: %s\n' "$file" >&2; exit 1; }
done

test "$("$llama" version)" = "b9986-91c631b21"
test "$(shasum -a 256 "$llama" | awk '{print $1}')" = "15d468c9f4820f4ba0fd44ccd2e19dd19bb60e8c7c839deb3fc2cfbff281307c"
test "$(shasum -a 256 "$model" | awk '{print $1}')" = "aa473d51f451a22f0fcf39ba3330c14bed38a385712b1113440f69df4047a173"

port="$("$python" - <<'PY'
import socket
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)"
endpoint="http://127.0.0.1:$port"
umask 077
"$python" - <<'PY' >"$key_file"
import secrets
print(secrets.token_urlsafe(32))
PY
printf '%s\n' "$endpoint" >"$endpoint_file"

"$llama" serve \
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

for _ in $(seq 1 120); do
  # llama.app may answer 503 before the model is ready; retry until 200.
  status="$(curl --silent --output "$gate_root/models.json" --write-out '%{http_code}' --max-time 2     -H "Authorization: Bearer $(<"$key_file")"     "$endpoint/v1/models" || true)"
  if [[ "$status" == "200" && -s "$gate_root/models.json" ]]; then
    break
  fi
  sleep 1
done
test -s "$gate_root/models.json"

unauthenticated_status="$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 5 \
  -H 'Content-Type: application/json' \
  --data '{"content":"authentication gate"}' \
  "$endpoint/tokenize")"
if test "$unauthenticated_status" != "401"; then
  printf '%s\n' 'unauthenticated tokenize request was not rejected' >&2
  exit 1
fi

curl --silent --show-error --fail --max-time 10 \
  -H "Authorization: Bearer $(<"$key_file")" \
  -H 'Content-Type: application/json' \
  --data '{"content":"authentication gate"}' \
  "$endpoint/tokenize" >"$gate_root/tokenize.json"

HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 TOKENIZERS_PARALLELISM=false \
  "$python" "$repo_root/scripts/semantic/capture-parity.py" flagembedding \
  --cases "$cases" \
  --model-path "$reference_model" \
  --model BAAI/bge-m3 \
  --revision 5617a9f61b028005a4858fdac845db406aefb181 \
  --artifact-sha256 b5e0ce3470abf5ef3831aa1bd5553b486803e83251590ab7ff35a117cf6aad38 \
  --output "$gate_root/reference-capture.json"

"$python" "$repo_root/scripts/semantic/capture-parity.py" llama \
  --cases "$cases" \
  --endpoint "$endpoint" \
  --api-key-file "$key_file" \
  --model-alias worktrail-bge-m3-b9986-m1 \
  --model ggml-org/bge-m3-Q8_0-GGUF \
  --revision 9eba04c5d75ba5a1595e45de734d36bef4e5cb98 \
  --artifact-sha256 aa473d51f451a22f0fcf39ba3330c14bed38a385712b1113440f69df4047a173 \
  --output "$gate_root/candidate-capture.json"

go run "$repo_root/cmd/worktrail-semantic-eval" parity \
  --cases "$cases" \
  --reference "$gate_root/reference-capture.json" \
  --candidate "$gate_root/candidate-capture.json" \
  >"$gate_root/parity-report.json"

"$python" -m pip freeze >"$gate_root/reference-environment.txt"
{
  shasum -a 256 "$gate_root/llama-app.zst" "$llama" "$model"
  for file in "$reference_model"/* "$reference_model"/1_Pooling/*; do
    test -f "$file" && shasum -a 256 "$file"
  done
  shasum -a 256 \
    "$gate_root/reference-capture.json" \
    "$gate_root/candidate-capture.json" \
    "$gate_root/parity-report.json"
} >"$gate_root/hashes.txt"
