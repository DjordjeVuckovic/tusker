#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["duckdb>=1.1"]
# ///
"""Fetch news datasets from HuggingFace into datasets/<dir>/.

  uv run scripts/fetch_hf_dataset.py --list
  uv run scripts/fetch_hf_dataset.py cc_news --mirror        # shards to raw/
  uv run scripts/fetch_hf_dataset.py cc_news                 # whole corpus
  uv run scripts/fetch_hf_dataset.py cc_news 100000 500000   # nested slices
  uv run scripts/fetch_hf_dataset.py cc_news 50000 --format parquet

Sizes default to the whole corpus, and a count above it writes the whole corpus
instead of failing. csv is the only format datapipe preprocess reads today.

--mirror copies the shards down once. Slicing then reads them from disk: 482 s
to 1.4 s for a cc_news sample.

--mode sample seeds a reservoir sample, head takes the first N rows. Both
corpora ship in crawl order, so head is skewed: 273 of 8,759 cc_news domains in
the first 20k rows against 3,641 in a sample that size.

Slices are cut in hash(url) order so they nest. Dates go through TRY_CAST since
77,598 cc_news rows have none and CAST would abort the export.
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
EXT = {"csv": ".csv", "parquet": ".parquet"}
COPY_OPTIONS = {"csv": "HEADER, FORMAT CSV", "parquet": "FORMAT PARQUET"}


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
        return ROOT / "datasets" / self.out_dir / "raw"


def _published_at(column: str = "date") -> str:
    return f"strftime(TRY_CAST({column} AS TIMESTAMP), '%Y-%m-%d %H:%M:%S') AS date"


DATASETS: tuple[Dataset, ...] = (
    Dataset(
        key="cc_news",
        repo="vblagoje/cc_news",
        files="plain_text/*.parquet",
        out_dir="cc-news",
        columns=("title", "text", "description", "url", _published_at(), "domain"),
        summary="CC-News, 708,241 English articles, 2017-01..2019-08, 8,759 domains (~1.1 GB)",
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
        summary="All the News 2.0, 2,688,878 articles, 27 US outlets, 2016-01..2020-04 (~5.3 GB)",
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


def mb(path: Path) -> float:
    return path.stat().st_size / 1024 / 1024


def connect(temp_dir: str) -> duckdb.DuckDBPyConnection:
    con = duckdb.connect()
    con.execute("INSTALL httpfs; LOAD httpfs;")
    # Keep DuckDB's spill files out of the repo.
    con.execute(f"SET temp_directory = '{temp_dir}'")
    if sys.stderr.isatty():
        con.execute("SET enable_progress_bar = true")
    token = os.environ.get("HF_TOKEN")
    if token:
        escaped = token.replace("'", "''")
        con.execute(f"CREATE SECRET hf (TYPE huggingface, TOKEN '{escaped}')")
    return con


def download(hf_path: str, out: Path) -> bool:
    """False when out already holds the whole file."""
    owner, name, path = hf_path.removeprefix("hf://datasets/").split("/", 2)
    url = f"https://huggingface.co/datasets/{owner}/{name}/resolve/main/{path}"
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
    written = []
    for index, (hf_path,) in enumerate(listing, start=1):
        out = dest / Path(hf_path).name
        started = time.monotonic()
        fetched = download(hf_path, out)
        note = f"{time.monotonic() - started:,.1f}s" if fetched else "already mirrored"
        print(f"  [{index}/{len(listing)}] {out.name}  {mb(out):,.1f} MB  {note}", flush=True)
        written.append(out)

    write_meta(dest / "mirror.meta.json", ds, shards=[path.name for path in written])
    return written


def source_for(ds: Dataset) -> tuple[str, str]:
    if any(ds.mirror_dir.glob("*.parquet")):
        return f"{ds.mirror_dir}/*.parquet", f"mirror {rel(ds.mirror_dir)}"
    return ds.source, ds.source


def write_meta(out: Path, ds: Dataset, **fields: object) -> None:
    meta = {
        "tool": "scripts/fetch_hf_dataset.py",
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "duckdb_version": duckdb.__version__,
        "dataset": ds.key,
        "hf_repo": ds.repo,
        "columns": list(ds.columns),
        **fields,
    }
    out.write_text(json.dumps(meta, indent=2) + "\n")


def slice_dataset(
    con: duckdb.DuckDBPyConnection, ds: Dataset, args: argparse.Namespace, out_dir: Path
) -> list[Path]:
    out_dir.mkdir(parents=True, exist_ok=True)
    source, source_label = source_for(ds)
    print(f"→ {ds.key}: reading {source_label}", flush=True)

    scan = f"SELECT {', '.join(ds.columns)} FROM read_parquet('{source}')"
    sizes = sorted({s for s in args.sizes if s != FULL})
    wants_full = FULL in args.sizes
    written: list[Path] = []

    if sizes:
        total = con.execute(f"SELECT count(*) FROM read_parquet('{source}')").fetchone()[0]
        oversized = [s for s in sizes if s >= total]
        if oversized:
            print(f"!  {max(oversized):,} rows requested of {total:,}, writing the whole corpus")
            sizes = [s for s in sizes if s < total]
            wants_full = True

    def emit(size: int | str, query: str) -> None:
        name = f"dataset-{label(size)}{EXT[args.format]}"
        out = Path(args.output) if args.output else out_dir / name
        started = time.monotonic()
        rows = con.execute(f"COPY ({query}) TO '{out}' ({COPY_OPTIONS[args.format]})").fetchone()
        rows = int(rows[0]) if rows else 0
        write_meta(
            out.with_suffix(".meta.json"),
            ds,
            source=source,
            format=args.format,
            mode=args.mode,
            seed=args.seed if args.mode == "sample" and size != FULL else None,
            rows_requested=size,
            rows_written=rows,
        )
        print(f"  {rel(out)}  {rows:,} rows  {mb(out):,.1f} MB  {time.monotonic() - started:,.1f}s")
        written.append(out)

    if wants_full:
        emit(FULL, scan)
    if not sizes:
        return written

    if args.mode == "head":
        for size in sizes:
            emit(size, f"{scan} LIMIT {size}")
        return written

    started = time.monotonic()
    con.execute(
        f"CREATE TEMP TABLE slice AS {scan} "
        f"USING SAMPLE reservoir({sizes[-1]} ROWS) REPEATABLE ({args.seed})"
    )
    print(f"  sampled {sizes[-1]:,} rows in {time.monotonic() - started:,.1f}s")
    # Parallel scans have no stable row order; hash(url) keeps slices nested.
    for size in sizes:
        emit(size, f"SELECT * FROM slice ORDER BY hash(url) LIMIT {size}")
    return written


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="fetch_hf_dataset.py",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        description="Fetch HuggingFace news datasets into datasets/<dir>/.",
        epilog="\n".join(f"  {ds.key:<14} {ds.repo}\n{'':<17}{ds.summary}" for ds in DATASETS),
    )
    parser.add_argument("dataset", nargs="?", help="dataset key or HuggingFace repo id")
    parser.add_argument(
        "sizes",
        nargs="*",
        type=parse_size,
        default=None,
        help="row counts to slice, or 'full' (default: full)",
    )
    parser.add_argument(
        "--mirror", action="store_true", help="copy the shards to datasets/<dir>/raw/, no slicing"
    )
    parser.add_argument("--format", choices=tuple(EXT), help="slice encoding (default: csv)")
    parser.add_argument("--mode", choices=("sample", "head"), default="sample")
    parser.add_argument("--seed", type=int, default=42, help="reservoir seed (default: 42)")
    parser.add_argument("-o", "--output", help="exact output path; one size only")
    parser.add_argument("--out-dir", help="write into this directory instead")
    parser.add_argument("--list", action="store_true", help="list known datasets and exit")
    args = parser.parse_args(argv)

    if args.list:
        return args
    if not args.dataset:
        parser.error("dataset is required (--list shows the known keys)")
    if args.output and args.out_dir:
        parser.error("--output and --out-dir are mutually exclusive")

    if args.mirror:
        if args.sizes:
            parser.error("--mirror copies whole shards, drop the size argument")
        if args.output:
            parser.error("--mirror writes a directory of shards, use --out-dir")
        if args.format:
            parser.error("--mirror keeps the published parquet, --format does not apply")
        return args

    args.format = args.format or "csv"
    if not args.sizes:
        args.sizes = [FULL]
    if args.output:
        if len(args.sizes) > 1:
            parser.error("--output names a single file, pass one size or use --out-dir")
        if Path(args.output).suffix != EXT[args.format]:
            parser.error(f"--output must end in {EXT[args.format]} for --format {args.format}")
    return args


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    if args.list:
        for ds in DATASETS:
            state = "mirrored" if any(ds.mirror_dir.glob("*.parquet")) else "not mirrored"
            print(f"{ds.key:<14} {ds.repo} ({state})\n{'':<14} {ds.summary}")
        return 0

    ds = resolve(args.dataset)
    if args.output:
        out_dir = Path(args.output).parent
    elif args.out_dir:
        out_dir = Path(args.out_dir)
    else:
        out_dir = ROOT / "datasets" / ds.out_dir

    with tempfile.TemporaryDirectory(prefix="fetch-hf-dataset-") as temp_dir:
        con = connect(temp_dir)
        try:
            if args.mirror:
                written = mirror(con, ds, out_dir if args.out_dir else ds.mirror_dir)
            else:
                written = slice_dataset(con, ds, args, out_dir)
        finally:
            con.close()

    print(f"Done 🎇  {len(written)} file(s), {sum(mb(p) for p in written):,.1f} MB")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
