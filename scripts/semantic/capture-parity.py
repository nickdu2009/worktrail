#!/usr/bin/env python3
"""Capture local BGE-M3 embeddings without downloading or publishing anything."""

import argparse
import json
import math
import os
import urllib.request


CONFIG = {
    "dimension": 1024,
    "tokenizer": "BAAI/bge-m3@5617a9f61b028005a4858fdac845db406aefb181",
    "pooling": "cls",
    "normalization": "l2",
    "query_template": "{text}",
    "document_template": "{text}",
}


def arguments():
    parser = argparse.ArgumentParser()
    parser.add_argument("provider", choices=("flagembedding", "llama"))
    parser.add_argument("--cases", required=True)
    parser.add_argument("--model-path")
    parser.add_argument("--endpoint")
    parser.add_argument("--api-key-file")
    parser.add_argument("--model-alias")
    parser.add_argument("--model", required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--artifact-sha256", required=True)
    parser.add_argument("--output", required=True)
    return parser.parse_args()


def load_cases(path):
    with open(path, encoding="utf-8") as source:
        cases = json.load(source)
    if cases["schema"] != "worktrail.semantic.eval.corpus.v1":
        raise ValueError("unexpected corpus schema")
    return cases


def records(cases):
    for query in cases["queries"]:
        yield query["id"], query["text"]
    for document in cases["documents"]:
        yield document["id"], document["text"]


def normalized(vector):
    norm = math.sqrt(sum(value * value for value in vector))
    if not math.isfinite(norm) or norm == 0:
        raise ValueError("embedding has invalid norm")
    return [value / norm for value in vector]


def capture_flagembedding(args, entries):
    if not args.model_path:
        raise ValueError("--model-path is required for flagembedding")
    from FlagEmbedding import BGEM3FlagModel

    model = BGEM3FlagModel(args.model_path, use_fp16=False)
    texts = [text for _, text in entries]
    singles = []
    for text in texts:
        singles.append(model.encode([text], batch_size=1, max_length=8192)["dense_vecs"][0].tolist())
    batches = model.encode(texts, batch_size=len(texts), max_length=8192)["dense_vecs"].tolist()
    return singles, batches


def request_embeddings(endpoint, api_key, model_alias, inputs):
    payload = json.dumps({"model": model_alias, "input": inputs}).encode("utf-8")
    request = urllib.request.Request(
        endpoint.rstrip("/") + "/v1/embeddings",
        data=payload,
        headers={
            "Authorization": "Bearer " + api_key,
            "Content-Type": "application/json",
        },
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=120) as response:
        decoded = json.load(response)
    data = decoded.get("data", [])
    if len(data) != len(inputs):
        raise ValueError("embedding response count mismatch")
    return [item["embedding"] for item in data]


def capture_llama(args, entries):
    if not args.endpoint or not args.api_key_file or not args.model_alias:
        raise ValueError("--endpoint, --api-key-file, and --model-alias are required for llama")
    with open(args.api_key_file, encoding="utf-8") as key_file:
        api_key = key_file.read().strip()
    if not api_key:
        raise ValueError("API key file is empty")
    texts = [text for _, text in entries]
    singles = [request_embeddings(args.endpoint, api_key, args.model_alias, [text])[0] for text in texts]
    batches = request_embeddings(args.endpoint, api_key, args.model_alias, texts)
    return singles, batches


def main():
    args = arguments()
    cases = load_cases(args.cases)
    entries = list(records(cases))
    if args.provider == "flagembedding":
        singles, batches = capture_flagembedding(args, entries)
    else:
        singles, batches = capture_llama(args, entries)
    embeddings = []
    for (identifier, _), single, batch in zip(entries, singles, batches):
        embeddings.append(
            {
                "id": identifier,
                "single": normalized([float(value) for value in single]),
                "batch": normalized([float(value) for value in batch]),
            }
        )
    report = {
        "schema": "worktrail.semantic.eval.capture.v1",
        "provider": args.provider,
        "model": args.model,
        "revision": args.revision,
        "artifact_sha256": args.artifact_sha256,
        "config": CONFIG,
        "embeddings": embeddings,
    }
    with open(args.output, "w", encoding="utf-8") as destination:
        json.dump(report, destination, ensure_ascii=False, indent=2)
        destination.write("\n")


if __name__ == "__main__":
    os.environ.setdefault("HF_HUB_OFFLINE", "1")
    os.environ.setdefault("TRANSFORMERS_OFFLINE", "1")
    main()
