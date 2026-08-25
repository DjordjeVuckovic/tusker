#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# ///
"""Project a canonical JSONL down to the fields the embedder reads, gzipped.

  uv run scripts/slim_corpus.py <corpus.jsonl> [--full-content]

embed_corpus.py reads only id, title and description. The article body is most
of the bytes, so dropping it shrinks a cloud upload by an order of magnitude.
The .gz output is read directly by embed_corpus.py.

--full-content keeps the body, and has to be matched by --full-content on the
embedding side.

Ids are random UUIDs assigned at preprocess time, not hashes, so they cannot be
regenerated from the upstream dataset. They have to travel with the text or
nothing will match articles.id on load.
"""

import argparse
import gzip
import json
import sys
from pathlib import Path

FIELDS = ("id", "title", "description")


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("input", type=Path, help="canonical JSONL")
    p.add_argument("-o", "--out", type=Path, help="default: <input>-slim.jsonl.gz")
    p.add_argument("--full-content", action="store_true", help="also keep the article body")
    p.add_argument("--limit", type=int, help="stop after N documents")
    return p.parse_args()


def main() -> None:
    args = parse_args()
    if not args.input.is_file():
        sys.exit(f"no such file: {args.input}")

    stem = args.input.name.removesuffix(".gz").removesuffix(".jsonl")
    out_path = args.out or args.input.parent / f"{stem}-slim.jsonl.gz"

    written = 0
    with gzip.open(out_path, "wt", encoding="utf-8", compresslevel=6) as out:
        with open(args.input, encoding="utf-8") as fh:
            for lineno, line in enumerate(fh, 1):
                if not line.strip():
                    continue
                if args.limit is not None and written >= args.limit:
                    break

                record = json.loads(line)
                if "id" not in record:
                    sys.exit(f"{args.input}:{lineno} has no 'id' field")

                slim = {f: record.get(f) or "" for f in FIELDS}
                if args.full_content:
                    slim["content"] = record.get("full_content") or record.get("content") or ""

                out.write(json.dumps(slim, ensure_ascii=False))
                out.write("\n")
                written += 1

    before = args.input.stat().st_size
    after = out_path.stat().st_size
    print(f"{written:,} docs -> {out_path}")
    print(f"{before / 1e6:.1f} MB -> {after / 1e6:.1f} MB  ({before / after:.1f}x smaller)")


if __name__ == "__main__":
    main()
