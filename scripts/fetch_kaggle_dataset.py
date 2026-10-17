#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["kagglehub>=1.0", "kagglesdk>=0.1.22,<1.0"]
# ///
"""Fetch Kaggle datasets into datasets/<dir>/raw/.

  uv run scripts/fetch_kaggle_dataset.py --list
  uv run scripts/fetch_kaggle_dataset.py global_news
  uv run scripts/fetch_kaggle_dataset.py global_news --version 3
  uv run scripts/fetch_kaggle_dataset.py rmisra/news-category-dataset --force

Kaggle serves whatever files the owner uploaded, so there is nothing to project
or slice. That is why this is separate from fetch_hf_dataset.py, whose sizes,
--mode and --format flags mean nothing against a zip of CSVs.

kagglehub downloads one dataset version into its cache, and the files are
copied from there into raw/ next to raw/datapackage.json, which records the
handle, the version and each file's sha256. A file already in raw/ with the
same hash is kept, and one that differs is refused unless --force, which
clears raw/ and nothing else.

Credentials: anything kagglehub reads (KAGGLE_USERNAME and KAGGLE_KEY,
KAGGLE_API_TOKEN, ~/.kaggle/kaggle.json), or the ~/.kaggle/credentials.json
that `kaggle auth login` writes.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import sys
import time
from dataclasses import dataclass
from pathlib import Path

import kagglehub
import kagglehub.config
from kagglesdk import KaggleClient, KaggleCredentials

ROOT = Path(__file__).resolve().parents[1]
DATASETS_ROOT = ROOT / "datasets"
PROFILE = "https://datapackage.org/profiles/2.0/datapackage.json"
DESCRIPTOR = "datapackage.json"
CHUNK = 1024 * 1024

CREDENTIALS_HELP = """Kaggle credentials not found. Either:

  kaggle auth login                (writes ~/.kaggle/credentials.json)

or export a key:

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
    try:
        if kagglehub.config.get_kaggle_credentials() is not None:
            return
    except ValueError as error:
        raise SystemExit(str(error)) from error
    login = KaggleCredentials.load(KaggleClient())
    if login is None:
        raise SystemExit(CREDENTIALS_HELP)
    # kagglehub never reads credentials.json, so pass its access token the way
    # kagglehub reads one. An expired token is refreshed and saved back first.
    os.environ["KAGGLE_API_TOKEN"] = login.get_access_token()


def version_of(landed: Path) -> str:
    """kagglehub caches a dataset under .../versions/<n>, the version it resolved."""
    match = re.search(r"/versions/(\d+)$", landed.as_posix())
    if not match:
        raise SystemExit(f"cannot tell which version kagglehub fetched into {landed}")
    return match.group(1)


def sha256_of(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while chunk := handle.read(CHUNK):
            digest.update(chunk)
    return digest.hexdigest()


def refuse(path: Path, reason: str) -> SystemExit:
    return SystemExit(f"{rel(path)} {reason}; rerun with --force to clear raw/ and fetch again")


def clear(raw: Path) -> None:
    for entry in raw.iterdir():
        if entry.is_dir() and not entry.is_symlink():
            shutil.rmtree(entry)
        else:
            entry.unlink()


def recorded_version(raw: Path) -> str | None:
    descriptor = raw / DESCRIPTOR
    if not descriptor.exists():
        return None
    sources = json.loads(descriptor.read_text()).get("sources") or [{}]
    return sources[0].get("version")


def check_resource_path(relative: str) -> None:
    """Refuse a path internal/datapackage would reject, or a descriptor of its own.

    An uploaded datapackage.json, at any depth, would read as this fetch's
    provenance and be committed by the datasets/ ignore rules.
    """
    if relative[0] in "./~" or ":" in relative or "\\" in relative:
        raise SystemExit(
            f"{relative}: a data package path cannot start with '.', '/' or '~' or hold ':' or"
            " '\\'; remove the file from the upload or fetch it by hand"
        )
    if Path(relative).name == DESCRIPTOR:
        raise SystemExit(
            f"{relative}: the upload ships its own {DESCRIPTOR}, which raw/ reserves for this"
            " fetch's descriptor; fetch it by hand"
        )


def copy_hashed(source: Path, target: Path) -> str:
    target.parent.mkdir(parents=True, exist_ok=True)
    partial = target.with_name(target.name + ".part")
    digest = hashlib.sha256()
    try:
        with source.open("rb") as reader, partial.open("wb") as writer:
            while chunk := reader.read(CHUNK):
                digest.update(chunk)
                writer.write(chunk)
        partial.replace(target)
    finally:
        partial.unlink(missing_ok=True)
    return digest.hexdigest()


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


def fetch(ds: Dataset, *, version: int | None, raw: Path, force: bool) -> None:
    handle = ds.handle if version is None else f"{ds.handle}/versions/{version}"
    print(f"→ {ds.key}: downloading {handle} into the kagglehub cache", flush=True)
    started = time.monotonic()
    landed = Path(kagglehub.dataset_download(handle))
    fetched_version = version_of(landed)
    sources = sorted(path for path in landed.rglob("*") if path.is_file())
    if not sources:
        raise SystemExit(f"{handle} downloaded no files into {landed}")

    for source in sources:
        check_resource_path(source.relative_to(landed).as_posix())
    hashes = {source: sha256_of(source) for source in sources}

    raw.mkdir(parents=True, exist_ok=True)
    if force:
        clear(raw)
    elif recorded_version(raw) not in (None, fetched_version):
        raise refuse(
            raw / DESCRIPTOR, f"records version {recorded_version(raw)}, not {fetched_version}"
        )
    for source, digest in hashes.items():
        target = raw / source.relative_to(landed)
        if target.exists() and sha256_of(target) != digest:
            raise refuse(target, f"differs from {ds.handle} version {fetched_version}")

    resources = []
    for source in sources:
        relative = source.relative_to(landed).as_posix()
        target = raw / relative
        if not target.exists():
            copy_hashed(source, target)
        print(f"  {rel(target)}  {target.stat().st_size / 1024 / 1024:,.1f} MB")
        resources.append(
            {
                "name": relative,
                "path": relative,
                "format": source.suffix.removeprefix(".") or "bin",
                "bytes": target.stat().st_size,
                "hash": f"sha256:{hashes[source]}",
            }
        )

    write_descriptor(
        raw,
        {
            "$schema": PROFILE,
            "name": f"{ds.out_dir}-raw",
            "producedBy": {"tool": "scripts/fetch_kaggle_dataset.py"},
            "sources": [
                {
                    "title": f"Kaggle {ds.handle}",
                    "path": f"https://www.kaggle.com/datasets/{ds.handle}",
                    "version": fetched_version,
                }
            ],
            "resources": resources,
        },
    )
    print(f"\nDone 🎇  {len(sources)} file(s) in {time.monotonic() - started:,.1f}s")


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="fetch_kaggle_dataset.py",
        description="Fetch Kaggle datasets into datasets/<dir>/.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="\n".join(f"  {ds.key:<14} {ds.handle}\n{'':<17}{ds.summary}" for ds in DATASETS),
    )
    parser.add_argument("dataset", nargs="?", help="dataset key or Kaggle owner/slug handle")
    parser.add_argument("--version", type=int, help="dataset version (default: the latest)")
    parser.add_argument(
        "--datasets-root",
        default=str(DATASETS_ROOT),
        help="folder holding the dataset folders (default: datasets/)",
    )
    parser.add_argument(
        "--force", action="store_true", help="clear raw/ before copying the version in"
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
    raw = Path(args.datasets_root).expanduser() / ds.out_dir / "raw"
    fetch(ds, version=args.version, raw=raw, force=args.force)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
