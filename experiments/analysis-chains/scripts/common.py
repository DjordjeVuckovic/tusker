"""Shared I/O for the analyzer experiment: samples in, token dumps out."""

import gzip
import os
import json
from collections.abc import Iterator
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
OUT = ROOT / "out"
TOKENS = Path(os.environ.get("ANA_TOKENS_DIR", OUT / "tokens"))
FIELDS = ("title", "description", "content")

PG = {"host": "localhost", "port": 55701, "user": "postgres", "password": "x", "dbname": "postgres"}
TIGER = {**PG, "port": 55702}
PARADEDB = {**PG, "port": 55703}
ES = "http://localhost:55704"


def read_sample(name: str) -> list[dict]:
    with (OUT / f"sample_{name}.jsonl").open(encoding="utf-8") as handle:
        return [json.loads(line) for line in handle]


def write_dump(chain: str, rows: Iterator[dict]) -> Path:
    """Write one chain's token dump: one row per (line, field) with its term set and token count."""
    TOKENS.mkdir(parents=True, exist_ok=True)
    path = TOKENS / f"{chain}.jsonl.gz"
    with gzip.open(path, "wt", encoding="utf-8") as handle:
        for row in rows:
            handle.write(json.dumps(row, ensure_ascii=False, sort_keys=True) + "\n")
    return path


def read_dump(chain: str) -> dict[tuple[int, str], dict]:
    with gzip.open(TOKENS / f"{chain}.jsonl.gz", "rt", encoding="utf-8") as handle:
        rows = (json.loads(line) for line in handle)
        return {(row["line"], row["field"]): row for row in rows}
