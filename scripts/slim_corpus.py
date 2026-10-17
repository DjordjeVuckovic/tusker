#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = [
#   "torch>=2.4",
#   "transformers==4.51.3",
#   "numpy>=1.26",
#   "pyarrow>=16",
# ]
# ///
"""Project a canonical package down to the columns embed_corpus.py reads.

  uv run scripts/slim_corpus.py datasets/cc-news/canonical/<version>/datapackage.json --out /tmp/tusker-payload

Writes a package of its own to --out: articles.parquet holding id, title and
description in canonical row order, and a slim.datapackage.json whose `canonical`
property names the canonical package it came from. embed_corpus.py reads that
property, so embedding the projection yields the same package version and bytes
as embedding the canonical package. The article body is most of the bytes, so
this is what goes to the Kaggle dataset.

The output is plain parquet next to its descriptor: Kaggle decompresses .gz
uploads, which would break the recorded hash.
"""

import argparse
import itertools
import sys
from collections.abc import Iterator
from pathlib import Path
from typing import Any

import pyarrow as pa
import pyarrow.parquet as pq
from embed_corpus import (
    DATAPACKAGE_PROFILE,
    SLIM_DESCRIPTOR,
    PackageError,
    SourcePackage,
    describe_parquet_resource,
    iter_jsonl_records,
    iter_parquet_records,
    open_package,
    publish_package,
    read_descriptor,
    write_descriptor,
)

SLIM_SCHEMA = pa.schema(
    [(field, pa.string()) for field in ("id", "title", "description")]
)
BATCH_ROWS = 8192


def iter_slim_batches(source: SourcePackage) -> Iterator[pa.Table]:
    records = (
        iter_parquet_records(source.resource)
        if source.resource_format == "parquet"
        else iter_jsonl_records(source.resource)
    )
    while batch := list(itertools.islice(records, BATCH_ROWS)):
        columns = {
            field: [record.get(field) or "" for record in batch]
            for field in SLIM_SCHEMA.names
        }
        yield pa.Table.from_pydict(columns, schema=SLIM_SCHEMA)


def write_slim_package(
    source: SourcePackage, carried: dict[str, Any], directory: Path
) -> None:
    resource_path = directory / "articles.parquet"
    with pq.ParquetWriter(resource_path, SLIM_SCHEMA, compression="zstd") as writer:
        for table in iter_slim_batches(source):
            writer.write_table(table)

    descriptor: dict[str, Any] = {
        "$schema": DATAPACKAGE_PROFILE,
        "name": f"{source.canonical.name}-slim",
        "version": source.canonical.version,
        **carried,
        "canonical": source.canonical.to_descriptor(),
        "resources": [describe_parquet_resource(resource_path, name="articles")],
    }
    write_descriptor(directory, descriptor, name=SLIM_DESCRIPTOR)


def parse_args() -> argparse.Namespace:
    # No abbreviations: a stale flag such as --out must not quietly become --output-root.
    parser = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        allow_abbrev=False,
    )
    parser.add_argument("package", type=Path, help="canonical datapackage.json")
    parser.add_argument(
        "--out", type=Path, required=True, help="new directory for the slim package"
    )
    return parser.parse_args()


def run(args: argparse.Namespace) -> None:
    source = open_package(args.package)
    descriptor = read_descriptor(args.package)
    carried = {
        key: descriptor[key]
        for key in ("articleSchema", "language")
        if key in descriptor
    }

    args.out.parent.mkdir(parents=True, exist_ok=True)
    publish_package(
        args.out,
        lambda directory: write_slim_package(source, carried, directory),
        descriptor_name=SLIM_DESCRIPTOR,
    )

    before = source.resource.stat().st_size
    after = (args.out / "articles.parquet").stat().st_size
    print(
        f"{args.out}: {before / 1e6:.1f} MB -> {after / 1e6:.1f} MB  ({before / after:.1f}x smaller)"
    )


def main() -> None:
    try:
        run(parse_args())
    except PackageError as error:
        sys.exit(f"slim_corpus: {error}")


if __name__ == "__main__":
    main()
