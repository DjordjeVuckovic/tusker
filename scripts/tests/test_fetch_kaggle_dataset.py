"""fetch_kaggle_dataset.py, run as a CLI against a prepopulated kagglehub cache.

  uv run --with pytest --with kagglehub pytest scripts/tests/test_fetch_kaggle_dataset.py

kagglehub serves a pinned version from its cache without a request; any
request that escapes to the network hits a dead proxy and fails.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any

import pytest

SCRIPT = Path(__file__).resolve().parents[1] / "fetch_kaggle_dataset.py"

ARTICLES = b"title,text\nfirst,body\n"
ARTICLES_HASH = "sha256:2c92f3c5112cf88622ecf84ab749f868db4c0646c7e5954f36746f3e71653a43"
NOTES = b"notes\n"
NOTES_HASH = "sha256:444e0fffbd825e9610ff5b199485707a0c895339ae80c15cc8a8aee41b106fda"


@pytest.fixture
def cache(tmp_path: Path) -> Path:
    """kagglehub's cache layout for acme/news version 3, marked complete."""
    root = tmp_path / "kagglehub"
    version = root / "datasets" / "acme" / "news" / "versions" / "3"
    (version / "docs").mkdir(parents=True)
    (version / "articles.csv").write_bytes(ARTICLES)
    (version / "docs" / "notes.txt").write_bytes(NOTES)
    (root / "datasets" / "acme" / "news" / "3.complete").touch()
    return root


def fetch(cache: Path, datasets_root: Path, *extra: str) -> subprocess.CompletedProcess[str]:
    env = {
        **os.environ,
        "KAGGLEHUB_CACHE": str(cache),
        "KAGGLE_USERNAME": "fixture",
        "KAGGLE_KEY": "fixture",
        "HTTPS_PROXY": "http://127.0.0.1:9",
        "HTTP_PROXY": "http://127.0.0.1:9",
    }
    return subprocess.run(
        [
            sys.executable,
            str(SCRIPT),
            "acme/news",
            "--version",
            "3",
            "--datasets-root",
            str(datasets_root),
            *extra,
        ],
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )


def descriptor(raw: Path) -> dict[str, Any]:
    return json.loads((raw / "datapackage.json").read_text())


def test_fetch_copies_the_version_into_raw_with_a_descriptor(cache: Path, tmp_path: Path) -> None:
    datasets_root = tmp_path / "datasets"

    result = fetch(cache, datasets_root)

    assert result.returncode == 0, result.stderr
    raw = datasets_root / "news" / "raw"
    assert (raw / "articles.csv").read_bytes() == ARTICLES
    assert (raw / "docs" / "notes.txt").read_bytes() == NOTES
    assert [path.name for path in (datasets_root / "news").iterdir()] == ["raw"]

    package = descriptor(raw)
    assert package["$schema"] == "https://datapackage.org/profiles/2.0/datapackage.json"
    [source] = package["sources"]
    assert source["version"] == "3"
    assert "acme/news" in source["path"]
    resources = {resource["path"]: resource for resource in package["resources"]}
    assert resources.keys() == {"articles.csv", "docs/notes.txt"}
    articles = resources["articles.csv"]
    assert (articles["hash"], articles["bytes"], articles["format"]) == (ARTICLES_HASH, 22, "csv")
    assert resources["docs/notes.txt"]["hash"] == NOTES_HASH


def test_rerun_reproduces_the_descriptor(cache: Path, tmp_path: Path) -> None:
    datasets_root = tmp_path / "datasets"
    raw = datasets_root / "news" / "raw"

    first = fetch(cache, datasets_root)
    first_descriptor = (raw / "datapackage.json").read_bytes()
    second = fetch(cache, datasets_root)

    assert first.returncode == 0, first.stderr
    assert second.returncode == 0, second.stderr
    assert (raw / "datapackage.json").read_bytes() == first_descriptor


def test_a_differing_raw_file_is_refused_until_force_clears_raw(
    cache: Path, tmp_path: Path
) -> None:
    datasets_root = tmp_path / "datasets"
    dataset = datasets_root / "news"
    (dataset / "raw").mkdir(parents=True)
    (dataset / "raw" / "articles.csv").write_bytes(b"an older upload")
    (dataset / "raw" / "stale.csv").write_bytes(b"stale")
    (dataset / "mapping.yaml").write_text("kind: DataMapper\n")

    refused = fetch(cache, datasets_root)

    assert refused.returncode != 0
    assert "--force" in refused.stderr
    assert (dataset / "raw" / "articles.csv").read_bytes() == b"an older upload"
    assert not (dataset / "raw" / "datapackage.json").exists()

    forced = fetch(cache, datasets_root, "--force")

    assert forced.returncode == 0, forced.stderr
    assert (dataset / "raw" / "articles.csv").read_bytes() == ARTICLES
    assert not (dataset / "raw" / "stale.csv").exists()
    assert (dataset / "mapping.yaml").read_text() == "kind: DataMapper\n"


def test_a_file_no_descriptor_reader_accepts_is_refused(cache: Path, tmp_path: Path) -> None:
    version = cache / "datasets" / "acme" / "news" / "versions" / "3"
    (version / ".DS_Store").write_bytes(b"finder")
    datasets_root = tmp_path / "datasets"

    result = fetch(cache, datasets_root)

    assert result.returncode != 0
    assert ".DS_Store" in result.stderr
    assert not (datasets_root / "news" / "raw" / "datapackage.json").exists()
