#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["kagglehub>=1.0"]
# ///
"""Fetch Kaggle datasets into datasets/<dir>/.

Replaces the interactive scripts/download_kaggle_dataset.sh. The dataset is an
argument rather than a menu answer, so this is scriptable, and kagglehub, which
uv installs on demand, takes the place of the kaggle CLI.

Usage:
  uv run scripts/fetch_kaggle_dataset.py --list
  uv run scripts/fetch_kaggle_dataset.py global_news
  uv run scripts/fetch_kaggle_dataset.py rmisra/news-category-dataset --force

Kaggle serves whatever files the dataset owner uploaded, so there is nothing to
project or slice. That is why this stays separate from fetch_hf_dataset.py,
whose sizes, --mode, --seed and --format flags would all be meaningless against
a zip of CSVs. The two share only conventions: a keyed registry,
datasets/<dir>/ output, and a .meta.json sidecar.

Credentials: KAGGLE_USERNAME and KAGGLE_KEY in the environment, or
~/.kaggle/kaggle.json.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

import kagglehub

ROOT = Path(__file__).resolve().parents[1]

META_NAME = "dataset.meta.json"

CREDENTIALS_HELP = """Kaggle credentials not found. Either:

  export KAGGLE_USERNAME=<username> KAGGLE_KEY=<key>

or place the token file:

  1. https://www.kaggle.com/settings, API, Create New Token (downloads kaggle.json)
  2. mkdir -p ~/.kaggle && mv ~/Downloads/kaggle.json ~/.kaggle/
  3. chmod 600 ~/.kaggle/kaggle.json"""


@dataclass(frozen=True)
class Dataset:
    key: str
    handle: str
    out_dir: str
    summary: str


DATASETS: tuple[Dataset, ...] = (
    Dataset(
        key="global_news",
        handle="everydaycodings/global-news-dataset",
        out_dir="global-news-dataset",
        summary="Global News Dataset, the corpus the current benchmark tracks run against",
    ),
    Dataset(
        key="news_category",
        handle="rmisra/news-category-dataset",
        out_dir="news-category-dataset",
        summary="News Category Dataset, HuffPost headlines with category labels",
    ),
    Dataset(
        key="hacker_news",
        handle="hacker-news/hacker-news-posts",
        out_dir="hacker-news-posts",
        summary="Hacker News Posts, titles, scores and timestamps",
    ),
)


def resolve(name: str) -> Dataset:
    for ds in DATASETS:
        if name in (ds.key, ds.handle):
            return ds
    if "/" in name:
        slug = name.split("/", 1)[1]
        return Dataset(key=slug, handle=name, out_dir=slug, summary="ad-hoc handle")
    known = ", ".join(ds.key for ds in DATASETS)
    raise SystemExit(f"unknown dataset {name!r}, known keys: {known}, or pass an owner/slug handle")


def rel(path: Path) -> Path:
    try:
        return path.relative_to(ROOT)
    except ValueError:
        return path


def check_credentials() -> None:
    if os.environ.get("KAGGLE_USERNAME") and os.environ.get("KAGGLE_KEY"):
        return
    if (Path.home() / ".kaggle" / "kaggle.json").exists():
        return
    raise SystemExit(CREDENTIALS_HELP)


def write_meta(dest: Path, ds: Dataset, files: list[Path]) -> None:
    (dest / META_NAME).write_text(
        json.dumps(
            {
                "tool": "scripts/fetch_kaggle_dataset.py",
                "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
                "kagglehub_version": kagglehub.__version__,
                "dataset": ds.key,
                "kaggle_handle": ds.handle,
                "files": [path.name for path in files],
                "bytes": sum(path.stat().st_size for path in files),
            },
            indent=2,
        )
        + "\n"
    )


def fetch(ds: Dataset, dest: Path, force: bool) -> list[Path]:
    dest.mkdir(parents=True, exist_ok=True)
    print(f"→ {ds.key}: downloading {ds.handle} to {rel(dest)}", flush=True)
    started = time.monotonic()
    landed = Path(
        kagglehub.dataset_download(ds.handle, force_download=force, output_dir=str(dest))
    )
    files = sorted(
        path
        for path in landed.rglob("*")
        if path.is_file() and path.name not in {META_NAME, "README.md"}
    )
    if not files:
        raise SystemExit(f"{ds.handle} downloaded no files into {rel(landed)}")
    for path in files:
        print(f"  {rel(path)}  {path.stat().st_size / 1024 / 1024:,.1f} MB")
    write_meta(landed, ds, files)
    print(f"\nDone 🎇  {len(files)} file(s) in {time.monotonic() - started:,.1f}s")
    return files


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="fetch_kaggle_dataset.py",
        description="Fetch Kaggle datasets into datasets/<dir>/.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="\n".join(f"  {ds.key:<14} {ds.handle}\n{'':<17}{ds.summary}" for ds in DATASETS),
    )
    parser.add_argument("dataset", nargs="?", help="dataset key or Kaggle owner/slug handle")
    parser.add_argument("--out-dir", help="write into this directory instead")
    parser.add_argument(
        "--force", action="store_true", help="re-download even if the files are already there"
    )
    parser.add_argument("--list", action="store_true", help="list known datasets and exit")
    args = parser.parse_args(argv)
    if not args.list and not args.dataset:
        parser.error("dataset is required (use --list to see the known keys)")
    return args


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    if args.list:
        for ds in DATASETS:
            print(f"{ds.key:<14} {ds.handle}\n{'':<14} {ds.summary}")
        return 0

    ds = resolve(args.dataset)
    check_credentials()
    dest = Path(args.out_dir).expanduser() if args.out_dir else ROOT / "datasets" / ds.out_dir
    fetch(ds, dest, args.force)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
