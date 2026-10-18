#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["duckdb>=1.1"]
# ///
"""Fetch news datasets from HuggingFace into datasets/<dir>/.

  uv run scripts/fetch_hf_dataset.py --list
  uv run scripts/fetch_hf_dataset.py cc_news --mirror        # shards to raw/
  uv run scripts/fetch_hf_dataset.py cc_news --mirror --revision <sha>
  uv run scripts/fetch_hf_dataset.py cc_news                 # whole corpus
  uv run scripts/fetch_hf_dataset.py cc_news 100000 500000   # nested slices
  uv run scripts/fetch_hf_dataset.py cc_news 50000 --format parquet

Sizes default to the whole corpus, and a count above it writes the whole corpus
instead of failing. csv is the only format datapipe preprocess reads today.

--mirror copies one Hub commit's shards into raw/, checked against the Hub's
sha256; --force clears raw/ first. Slicing then reads them from disk: 482 s to
1.4 s for a cc_news sample.

--mode sample seeds a reservoir sample, head takes the first N rows. Both
corpora ship in crawl order, so head is skewed: 273 of 8,759 cc_news domains in
the first 20k rows against 3,641 in a sample that size.

Slices are cut in hash(url) order so they nest. Dates go through TRY_CAST since
77,598 cc_news rows have none and CAST would abort the export.
"""

from __future__ import annotations

import argparse
import fnmatch
import hashlib
import json
import os
import re
import shutil
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

import duckdb

ROOT = Path(__file__).resolve().parents[1]
DATASETS_ROOT = ROOT / "datasets"
HUB = "https://huggingface.co"
PROFILE = "https://datapackage.org/profiles/2.0/datapackage.json"
DESCRIPTOR = "datapackage.json"
CHUNK = 1024 * 1024

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

    def raw_dir(self, datasets_root: Path) -> Path:
        return datasets_root / self.out_dir / "raw"


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


@dataclass(frozen=True, slots=True)
class HubFile:
    path: str
    size: int
    sha256: str | None

    @property
    def name(self) -> str:
        return Path(self.path).name


def hub_endpoint() -> str:
    return os.environ.get("HF_ENDPOINT", HUB).rstrip("/")


def hub_request(url: str) -> urllib.request.Request:
    request = urllib.request.Request(url)
    token = os.environ.get("HF_TOKEN")
    if token:
        request.add_unredirected_header("Authorization", f"Bearer {token}")
    return request


def hub_json(url: str) -> tuple[object, str | None]:
    """Return the decoded body and the next page's url, if any."""
    try:
        with urllib.request.urlopen(hub_request(url)) as response:
            next_page = re.search(r'<([^>]+)>;\s*rel="next"', response.headers.get("Link") or "")
            return json.load(response), next_page.group(1) if next_page else None
    except urllib.error.HTTPError as error:
        raise SystemExit(f"GET {url}: {error.code} {error.reason}") from error


def resolve_commit(ds: Dataset, revision: str) -> str:
    quoted = urllib.parse.quote(revision, safe="")
    info, _ = hub_json(f"{hub_endpoint()}/api/datasets/{ds.repo}/revision/{quoted}")
    if not isinstance(info, dict) or not isinstance(info.get("sha"), str):
        raise SystemExit(f"{ds.repo}@{revision}: the Hub returned no commit sha")
    return info["sha"]


def list_hub_files(ds: Dataset, commit: str) -> list[HubFile]:
    directory = ds.files.split("*", 1)[0].rsplit("/", 1)[0]
    entries: list[dict] = []
    url: str | None = (
        f"{hub_endpoint()}/api/datasets/{ds.repo}/tree/{commit}/{directory}?recursive=true"
    )
    while url:
        page, url = hub_json(url)
        if not isinstance(page, list):
            raise SystemExit(f"{ds.repo}@{commit}: unexpected tree listing")
        entries.extend(page)
    files = sorted(
        (
            HubFile(
                path=entry["path"],
                size=int(entry["size"]),
                sha256=entry["lfs"]["oid"] if entry.get("lfs") else None,
            )
            for entry in entries
            if entry.get("type") == "file" and fnmatch.fnmatch(entry["path"], ds.files)
        ),
        key=lambda hub_file: hub_file.path,
    )
    if not files:
        raise SystemExit(f"no files matched {ds.files} in {ds.repo}@{commit}")
    names = [hub_file.name for hub_file in files]
    if len(set(names)) != len(names):
        raise SystemExit(f"{ds.files} matches files that share a name, raw/ cannot hold them flat")
    return files


def sha256_of(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while chunk := handle.read(CHUNK):
            digest.update(chunk)
    return digest.hexdigest()


def refuse(path: Path, reason: str) -> SystemExit:
    return SystemExit(f"{rel(path)} {reason}; rerun with --force to clear raw/ and fetch again")


def download_verified(url: str, partial: Path, hub_file: HubFile) -> str:
    digest = hashlib.sha256()
    size = 0
    try:
        with urllib.request.urlopen(hub_request(url)) as response, partial.open("wb") as handle:
            while chunk := response.read(CHUNK):
                digest.update(chunk)
                handle.write(chunk)
                size += len(chunk)
    except urllib.error.HTTPError as error:
        raise SystemExit(f"GET {url}: {error.code} {error.reason}") from error
    if size != hub_file.size:
        raise SystemExit(
            f"{hub_file.path}: received {size:,} bytes, the Hub lists {hub_file.size:,}"
        )
    if hub_file.sha256 and digest.hexdigest() != hub_file.sha256:
        raise SystemExit(
            f"{hub_file.path}: sha256 {digest.hexdigest()}, the Hub lists {hub_file.sha256}"
        )
    return digest.hexdigest()


def place(ds: Dataset, commit: str, hub_file: HubFile, target: Path) -> tuple[str, bool]:
    if target.exists() and hub_file.sha256:
        if sha256_of(target) != hub_file.sha256:
            raise refuse(target, f"differs from {ds.repo}@{commit}")
        return hub_file.sha256, False

    url = (
        f"{hub_endpoint()}/datasets/{ds.repo}/resolve/{commit}/{urllib.parse.quote(hub_file.path)}"
    )
    partial = target.with_name(target.name + ".part")
    try:
        digest = download_verified(url, partial, hub_file)
        if target.exists() and sha256_of(target) != digest:
            raise refuse(target, f"differs from {ds.repo}@{commit}")
        partial.replace(target)
    finally:
        partial.unlink(missing_ok=True)
    return digest, True


def clear(raw: Path) -> None:
    for entry in raw.iterdir():
        if entry.is_dir() and not entry.is_symlink():
            shutil.rmtree(entry)
        else:
            entry.unlink()


def recorded_commit(raw: Path) -> str | None:
    descriptor = raw / DESCRIPTOR
    if not descriptor.exists():
        return None
    sources = json.loads(descriptor.read_text()).get("sources") or [{}]
    return sources[0].get("version")


def write_descriptor(raw: Path, package: dict[str, object]) -> None:
    content = json.dumps(package, indent=2) + "\n"
    target = raw / DESCRIPTOR
    if target.exists():
        if target.read_text() == content:
            return
        raise refuse(target, "describes a different fetch")
    partial = raw / f".{DESCRIPTOR}.part"
    try:
        partial.write_text(content)
        partial.replace(target)
    finally:
        partial.unlink(missing_ok=True)


def mirror(ds: Dataset, *, revision: str, raw: Path, force: bool) -> list[Path]:
    commit = resolve_commit(ds, revision)
    hub_files = list_hub_files(ds, commit)

    raw.mkdir(parents=True, exist_ok=True)
    if force:
        clear(raw)
    elif recorded_commit(raw) not in (None, commit):
        raise refuse(raw / DESCRIPTOR, f"records {ds.repo}@{recorded_commit(raw)}, not {commit}")
    print(
        f"→ {ds.key}: mirroring {len(hub_files)} shards of {ds.repo}@{commit} to {rel(raw)}",
        flush=True,
    )
    resources = []
    written = []
    for index, hub_file in enumerate(hub_files, start=1):
        target = raw / hub_file.name
        started = time.monotonic()
        digest, fetched = place(ds, commit, hub_file, target)
        note = f"{time.monotonic() - started:,.1f}s" if fetched else "already mirrored"
        print(
            f"  [{index}/{len(hub_files)}] {target.name}  {mb(target):,.1f} MB  {note}", flush=True
        )
        resources.append(
            {
                "name": hub_file.name,
                "path": hub_file.name,
                "format": Path(hub_file.name).suffix.removeprefix(".") or "bin",
                "bytes": hub_file.size,
                "hash": f"sha256:{digest}",
            }
        )
        written.append(target)

    write_descriptor(
        raw,
        {
            "$schema": PROFILE,
            "name": f"{ds.out_dir}-raw",
            "version": commit,
            "producedBy": {"tool": "scripts/fetch_hf_dataset.py"},
            "sources": [
                {
                    "title": f"Hugging Face {ds.repo}",
                    "path": f"{HUB}/datasets/{ds.repo}",
                    "version": commit,
                }
            ],
            "resources": resources,
        },
    )
    return written


def source_for(ds: Dataset, datasets_root: Path) -> tuple[str, str]:
    raw = ds.raw_dir(datasets_root)
    if any(raw.glob("*.parquet")):
        return f"{raw}/*.parquet", f"mirror {rel(raw)}"
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
    source, source_label = source_for(ds, Path(args.datasets_root))
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
    parser.add_argument(
        "--revision", default="main", help="branch, tag or commit to mirror (default: main)"
    )
    parser.add_argument(
        "--force", action="store_true", help="with --mirror, clear raw/ before fetching"
    )
    parser.add_argument(
        "--datasets-root",
        default=str(DATASETS_ROOT),
        help="folder holding the dataset folders (default: datasets/)",
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
        if args.output or args.out_dir:
            parser.error("--mirror writes only into datasets/<dir>/raw/, use --datasets-root")
        if args.format:
            parser.error("--mirror keeps the published parquet, --format does not apply")
        return args

    if args.force or args.revision != "main":
        parser.error("--force and --revision apply to --mirror only")
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
            mirrored = any(ds.raw_dir(Path(args.datasets_root)).glob("*.parquet"))
            state = "mirrored" if mirrored else "not mirrored"
            print(f"{ds.key:<14} {ds.repo} ({state})\n{'':<14} {ds.summary}")
        return 0

    ds = resolve(args.dataset)
    datasets_root = Path(args.datasets_root)
    if args.mirror:
        raw = ds.raw_dir(datasets_root)
        written = mirror(ds, revision=args.revision, raw=raw, force=args.force)
        print(f"Done 🎇  {len(written)} file(s), {sum(mb(p) for p in written):,.1f} MB")
        return 0

    if args.output:
        out_dir = Path(args.output).parent
    elif args.out_dir:
        out_dir = Path(args.out_dir)
    else:
        out_dir = datasets_root / ds.out_dir

    with tempfile.TemporaryDirectory(prefix="fetch-hf-dataset-") as temp_dir:
        con = connect(temp_dir)
        try:
            written = slice_dataset(con, ds, args, out_dir)
        finally:
            con.close()

    print(f"Done 🎇  {len(written)} file(s), {sum(mb(p) for p in written):,.1f} MB")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
