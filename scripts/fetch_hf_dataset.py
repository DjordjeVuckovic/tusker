#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["duckdb>=1.1"]
# ///
"""Fetch news datasets from HuggingFace into datasets/<dir>/.

Reads parquet from HuggingFace with DuckDB and writes slices that keep the
source column names as the publisher wrote them. Renaming raw fields onto the
canonical schema happens later, in datapipe preprocess.

Usage:
  uv run scripts/fetch_hf_dataset.py --list
  uv run scripts/fetch_hf_dataset.py cc_news 100000
  uv run scripts/fetch_hf_dataset.py all_the_news 100000 500000 --mode head
  uv run scripts/fetch_hf_dataset.py cc_news --format raw
  uv run scripts/fetch_hf_dataset.py vblagoje/cc_news full -o datasets/cc-news/dataset.csv

--format picks what lands on disk. csv, the default, is the only raw input
datapipe preprocess reads today. parquet writes the same columns for when
internal/ingest/reader/parquet_reader.go stops being a stub. raw mirrors the
untouched HuggingFace shards into datasets/<dir>/raw/, after which every slice
reads from disk instead of re-streaming the corpus over the network, which
takes about 8 minutes for cc_news and several times that for all_the_news.

--mode sample, also the default, draws a seeded reservoir sample. Both corpora
are stored in crawl order, so a head slice is skewed: the first 20k cc_news
rows cover 273 of 8,759 domains, and the first 20k all_the_news rows cover 7 of
27 publications. That skew moves IDF and BM25 scores, which is what the
benchmarks measure. --mode head keeps the fast, biased path for smoke tests.

Sizes are cut from a single scan in hash(url) order, so a smaller slice is
always a prefix of a larger one. Dates go through TRY_CAST because 77,598 of
708,241 cc_news rows have an empty date, and a plain CAST aborts the whole
export on the first one. Slicing runs in DuckDB rather than head -n because
article bodies contain newlines and would be split mid-record.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import sys
import tempfile
import time
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

import duckdb

ROOT = Path(__file__).resolve().parents[1]

FULL = "full"
RAW = "raw"
FORMAT_EXT = {"csv": ".csv", "parquet": ".parquet"}


@dataclass(frozen=True)
class Dataset:
    key: str
    repo: str
    files: str
    out_dir: str
    columns: tuple[str, ...]
    summary: str

    @property
    def source(self) -> str:
        return f"hf://datasets/{self.repo}/{self.files}"

    @property
    def mirror_dir(self) -> Path:
        return ROOT / "datasets" / self.out_dir / RAW


def _published_at(column: str = "date") -> str:
    return f"strftime(TRY_CAST({column} AS TIMESTAMP), '%Y-%m-%d %H:%M:%S') AS date"


DATASETS: tuple[Dataset, ...] = (
    Dataset(
        key="cc_news",
        repo="vblagoje/cc_news",
        files="plain_text/*.parquet",
        out_dir="cc-news",
        columns=("title", "text", "description", "url", _published_at(), "domain"),
        summary="CC-News, 708,241 English articles, 2017-01..2019-08, "
        "8,759 domains, full bodies via news-please (~1.1 GB)",
    ),
    Dataset(
        key="all_the_news",
        repo="rjac/all-the-news-2-1-Component-one",
        files="data/*.parquet",
        out_dir="all-the-news",
        columns=(
            "title",
            "article",
            "author",
            "url",
            _published_at(),
            "publication",
            "section",
        ),
        summary="All the News 2.0, 2,688,878 articles, 27 US outlets, "
        "2016-01..2020-04, full bodies (~5.3 GB)",
    ),
)


def resolve(name: str) -> Dataset:
    for ds in DATASETS:
        if name in (ds.key, ds.repo):
            return ds
    known = ", ".join(ds.key for ds in DATASETS)
    raise SystemExit(f"unknown dataset {name!r}, known keys: {known}")


def parse_size(value: str) -> int | str:
    if value.lower() == FULL:
        return FULL
    try:
        size = int(value.replace("_", ""))
    except ValueError:
        raise argparse.ArgumentTypeError(f"expected a row count or 'full', got {value!r}")
    if size <= 0:
        raise argparse.ArgumentTypeError("row count must be positive")
    return size


def label(size: int | str) -> str:
    if size == FULL:
        return FULL
    if size % 1_000_000 == 0:
        return f"{size // 1_000_000}m"
    if size % 1_000 == 0:
        return f"{size // 1_000}k"
    return str(size)


def rel(path: Path) -> Path:
    try:
        return path.relative_to(ROOT)
    except ValueError:
        return path


def connect(threads: int | None, temp_dir: str) -> duckdb.DuckDBPyConnection:
    con = duckdb.connect()
    con.execute("INSTALL httpfs; LOAD httpfs;")
    # An in-memory database spills into the working directory, so send it elsewhere.
    con.execute(f"SET temp_directory = '{temp_dir}'")
    if threads:
        con.execute(f"SET threads = {threads}")
    if sys.stderr.isatty():
        con.execute("SET enable_progress_bar = true")
    token = os.environ.get("HF_TOKEN")
    if token:
        escaped = token.replace("'", "''")
        con.execute(f"CREATE SECRET hf (TYPE huggingface, TOKEN '{escaped}')")
    return con


def hf_download_url(hf_path: str) -> str:
    owner, name, path = hf_path.removeprefix("hf://datasets/").split("/", 2)
    return f"https://huggingface.co/datasets/{owner}/{name}/resolve/main/{path}"


def download(url: str, out: Path) -> bool:
    """Returns False when out already holds the whole file."""
    request = urllib.request.Request(url)
    token = os.environ.get("HF_TOKEN")
    if token:
        request.add_header("Authorization", f"Bearer {token}")
    with urllib.request.urlopen(request) as response:
        expected = int(response.headers.get("Content-Length") or 0)
        if out.exists() and expected and out.stat().st_size == expected:
            return False
        partial = out.with_suffix(out.suffix + ".part")
        with partial.open("wb") as handle:
            shutil.copyfileobj(response, handle, 1024 * 1024)
    partial.replace(out)
    return True


def mirror(con: duckdb.DuckDBPyConnection, ds: Dataset, dest: Path) -> list[Path]:
    listing = con.execute(f"SELECT file FROM glob('{ds.source}') ORDER BY file").fetchall()
    if not listing:
        raise SystemExit(f"no parquet files matched {ds.source}")

    dest.mkdir(parents=True, exist_ok=True)
    print(f"→ {ds.key}: mirroring {len(listing)} shards to {rel(dest)}", flush=True)
    written: list[Path] = []
    for index, (hf_path,) in enumerate(listing, start=1):
        out = dest / Path(hf_path).name
        started = time.monotonic()
        fetched = download(hf_download_url(hf_path), out)
        mb = out.stat().st_size / 1024 / 1024
        note = f"{time.monotonic() - started:,.1f}s" if fetched else "already mirrored"
        print(f"  [{index}/{len(listing)}] {out.name}  {mb:,.1f} MB  {note}", flush=True)
        written.append(out)

    (dest / "mirror.meta.json").write_text(
        json.dumps(
            {
                "tool": "scripts/fetch_hf_dataset.py",
                "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
                "dataset": ds.key,
                "hf_repo": ds.repo,
                "source": ds.source,
                "shards": [path.name for path in written],
                "bytes": sum(path.stat().st_size for path in written),
            },
            indent=2,
        )
        + "\n"
    )
    return written


def source_for(ds: Dataset) -> tuple[str, str]:
    if any(ds.mirror_dir.glob("*.parquet")):
        return f"{ds.mirror_dir}/*.parquet", f"local mirror {rel(ds.mirror_dir)}"
    return ds.source, ds.source


def copy_to_file(con: duckdb.DuckDBPyConnection, query: str, out: Path, fmt: str) -> int:
    options = "HEADER, FORMAT CSV" if fmt == "csv" else "FORMAT PARQUET"
    rows = con.execute(f"COPY ({query}) TO '{out}' ({options})").fetchone()
    return int(rows[0]) if rows else 0


def write_meta(
    out: Path,
    ds: Dataset,
    args: argparse.Namespace,
    source: str,
    size: int | str,
    rows: int,
) -> None:
    meta = {
        "tool": "scripts/fetch_hf_dataset.py",
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "duckdb_version": duckdb.__version__,
        "dataset": ds.key,
        "hf_repo": ds.repo,
        "source": source,
        "format": args.format,
        "mode": args.mode,
        "seed": args.seed if args.mode == "sample" and size != FULL else None,
        "rows_requested": size,
        "rows_written": rows,
        "columns": list(ds.columns),
    }
    out.with_suffix(".meta.json").write_text(json.dumps(meta, indent=2) + "\n")


def fetch(con: duckdb.DuckDBPyConnection, ds: Dataset, args: argparse.Namespace) -> list[Path]:
    if args.output:
        out_dir = Path(args.output).expanduser().parent
    elif args.out_dir:
        out_dir = Path(args.out_dir).expanduser()
    else:
        out_dir = ROOT / "datasets" / ds.out_dir

    if args.format == RAW:
        return mirror(con, ds, out_dir if args.out_dir else ds.mirror_dir)

    out_dir.mkdir(parents=True, exist_ok=True)
    source, source_label = source_for(ds)
    print(f"→ {ds.key}: reading {source_label}", flush=True)

    select = ", ".join(ds.columns)
    scan = f"SELECT {select} FROM read_parquet('{source}')"
    sizes = sorted({s for s in args.sizes if s != FULL})
    written: list[Path] = []

    if args.mode == "sample" and sizes and FULL in args.sizes:
        print("!  'full' and a sampled size together read the corpus twice", flush=True)

    def emit(size: int | str, query: str) -> None:
        out = (
            Path(args.output).expanduser()
            if args.output
            else out_dir / f"dataset-{label(size)}{FORMAT_EXT[args.format]}"
        )
        started = time.monotonic()
        print(f"→ {ds.key}: writing {label(size)} to {rel(out)}", flush=True)
        rows = copy_to_file(con, query, out, args.format)
        write_meta(out, ds, args, source, size, rows)
        mb = out.stat().st_size / 1024 / 1024
        print(f"  {rows:,} rows, {mb:,.1f} MB, {time.monotonic() - started:,.1f}s")
        written.append(out)

    if FULL in args.sizes:
        emit(FULL, scan)

    if not sizes:
        return written

    if args.mode == "head":
        for size in sizes:
            emit(size, f"{scan} LIMIT {size}")
        return written

    biggest = sizes[-1]
    wait = "" if source_label.startswith("local mirror") else ", which streams the whole corpus"
    print(f"→ {ds.key}: sampling {biggest:,} rows (seed {args.seed}){wait}", flush=True)
    started = time.monotonic()
    con.execute(
        f"CREATE TEMP TABLE slice AS {scan} "
        f"USING SAMPLE reservoir({biggest} ROWS) REPEATABLE ({args.seed})"
    )
    print(f"  sampled in {time.monotonic() - started:,.1f}s")
    # hash(url) rather than table order: DuckDB does not promise a stable row
    # order out of a parallel scan, and the slices have to nest.
    for size in sizes:
        emit(size, f"SELECT * FROM slice ORDER BY hash(url) LIMIT {size}")
    return written


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="fetch_hf_dataset.py",
        description="Fetch HuggingFace news datasets into datasets/<dir>/.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="\n".join(f"  {ds.key:<14} {ds.repo}\n{'':<17}{ds.summary}" for ds in DATASETS),
    )
    parser.add_argument("dataset", nargs="?", help="dataset key or HuggingFace repo id")
    parser.add_argument(
        "sizes",
        nargs="*",
        type=parse_size,
        default=None,
        help="row counts to cut, or 'full' (default: 100000)",
    )
    parser.add_argument(
        "--format",
        choices=("csv", "parquet", RAW),
        default="csv",
        help="csv: projected slice, the only input preprocess reads today (default). "
        "parquet: same columns, awaiting ParquetReader. "
        "raw: mirror the untouched shards to datasets/<dir>/raw/",
    )
    parser.add_argument(
        "--mode",
        choices=("sample", "head"),
        default="sample",
        help="sample: seeded reservoir sample over the whole corpus (default). "
        "head: first N rows, fast but crawl-ordered and biased",
    )
    parser.add_argument("--seed", type=int, default=42, help="reservoir sample seed (default: 42)")
    parser.add_argument(
        "-o",
        "--output",
        help="write the slice to this exact path instead of "
        "datasets/<dir>/dataset-<size>.<ext>; one size only",
    )
    parser.add_argument("--out-dir", help="write into this directory instead")
    parser.add_argument("--threads", type=int, help="DuckDB thread count")
    parser.add_argument("--list", action="store_true", help="list known datasets and exit")
    args = parser.parse_args(argv)
    if args.list:
        return args
    if not args.dataset:
        parser.error("dataset is required (use --list to see the known keys)")
    if args.output and args.out_dir:
        parser.error("--output and --out-dir are mutually exclusive")

    if args.format == RAW:
        if args.sizes:
            parser.error("--format raw mirrors whole shards, drop the size argument")
        if args.output:
            parser.error("--format raw writes a directory of shards, use --out-dir")
        return args

    if not args.sizes:
        args.sizes = [100_000]
    if args.output:
        if len(args.sizes) > 1:
            parser.error("--output names a single file, pass one size or use --out-dir")
        want = FORMAT_EXT[args.format]
        if Path(args.output).suffix != want:
            parser.error(f"--output must end in {want} for --format {args.format}")
    return args


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    if args.list:
        for ds in DATASETS:
            state = "mirrored" if any(ds.mirror_dir.glob("*.parquet")) else "not mirrored"
            print(f"{ds.key:<14} {ds.repo} ({state})\n{'':<14} {ds.summary}")
        return 0

    ds = resolve(args.dataset)
    with tempfile.TemporaryDirectory(prefix="fetch-hf-dataset-") as temp_dir:
        con = connect(args.threads, temp_dir)
        try:
            written = fetch(con, ds, args)
        finally:
            con.close()

    total = sum(path.stat().st_size for path in written) / 1024 / 1024
    print(f"\nDone 🎇  {len(written)} file(s), {total:,.1f} MB in {rel(written[0].parent)}")
    if args.format == RAW:
        print(f"  slices now read from disk: uv run scripts/fetch_hf_dataset.py {ds.key} 100000")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
