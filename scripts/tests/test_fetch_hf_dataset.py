"""fetch_hf_dataset.py --mirror, run as a CLI against a local fake of the Hub.

  uv run --with pytest --with duckdb pytest scripts/tests/test_fetch_hf_dataset.py

Any request that escapes to the real network hits a dead proxy and fails.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import threading
from collections.abc import Callable, Iterator
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

import pytest

SCRIPT = Path(__file__).resolve().parents[1] / "fetch_hf_dataset.py"
REPO = "vblagoje/cc_news"
COMMIT = "81eb2ce0d2a9dad6ad16b68ef750ec290880fa36"

SHARD_ZERO = b"PAR1 shard zero"
SHARD_ZERO_HASH = "sha256:b35fdcdbad89a58e029618c24bcfdd2df0eecd209633cecc68b1bd8e8227885b"
SHARD_ONE = b"PAR1 shard one"
SHARD_ONE_HASH = "sha256:f3d5fbb3cceeb15f0b2ba518e95591dd2172e7851eeaa4ae9250adf5f872812d"


@dataclass
class FakeHub:
    """Serves one dataset revision: the files are only reachable by commit sha."""

    files: dict[str, bytes] = field(
        default_factory=lambda: {
            "plain_text/train-00000-of-00002.parquet": SHARD_ZERO,
            "plain_text/train-00001-of-00002.parquet": SHARD_ONE,
            "plain_text/notes.txt": b"not a shard",
        }
    )
    served: dict[str, bytes] = field(default_factory=dict)
    endpoint: str = ""
    cdn: str = ""

    def tree(self) -> list[dict[str, object]]:
        hashes = {
            "plain_text/train-00000-of-00002.parquet": SHARD_ZERO_HASH,
            "plain_text/train-00001-of-00002.parquet": SHARD_ONE_HASH,
        }
        entries: list[dict[str, object]] = []
        for path, content in self.files.items():
            entry: dict[str, object] = {"type": "file", "path": path, "size": len(content)}
            if path in hashes:
                oid = hashes[path].removeprefix("sha256:")
                entry["lfs"] = {"oid": oid, "size": len(content)}
            entries.append(entry)
        return entries

    def respond(self, path: str) -> tuple[int, bytes, dict[str, str]]:
        """Answer like the Hub API, splitting the tree listing over two pages."""
        tree = f"/api/datasets/{REPO}/tree/{COMMIT}/plain_text?recursive=true"
        if path == f"/api/datasets/{REPO}/revision/main":
            return 200, json.dumps({"sha": COMMIT}).encode(), {}
        if path == tree:
            link = f'<{self.endpoint}{tree}&cursor=1>; rel="next"'
            return 200, json.dumps(self.tree()[:1]).encode(), {"Link": link}
        if path == f"{tree}&cursor=1":
            return 200, json.dumps(self.tree()[1:]).encode(), {}
        prefix = f"/datasets/{REPO}/resolve/{COMMIT}/"
        if path.startswith(prefix):
            name = path.removeprefix(prefix)
            if self.cdn:
                return 302, b"", {"Location": f"{self.cdn}/{name}"}
            return self.content(name)
        return 404, b"not found", {}

    def content(self, name: str) -> tuple[int, bytes, dict[str, str]]:
        content = self.served.get(name, self.files.get(name))
        if content is None:
            return 404, b"not found", {}
        return 200, content, {}


Responder = Callable[[str], tuple[int, bytes, dict[str, str]]]


@dataclass
class Server:
    endpoint: str
    authorizations: list[str | None]


def serve(respond: Responder) -> Iterator[Server]:
    """Run respond behind a local HTTP server, recording each Authorization header."""
    authorizations: list[str | None] = []

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            authorizations.append(self.headers.get("Authorization"))
            status, body, headers = respond(self.path)
            self.send_response(status)
            for name, value in headers.items():
                self.send_header(name, value)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, format: str, *args: object) -> None:
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    yield Server(f"http://127.0.0.1:{server.server_port}", authorizations)
    server.shutdown()
    server.server_close()


@pytest.fixture
def hub() -> Iterator[tuple[FakeHub, str]]:
    fake = FakeHub()
    for server in serve(fake.respond):
        fake.endpoint = server.endpoint
        yield fake, fake.endpoint


def fetch(
    endpoint: str, root: Path, *extra: str, token: str | None = None
) -> subprocess.CompletedProcess[str]:
    env = {
        **os.environ,
        "HF_ENDPOINT": endpoint,
        "HTTPS_PROXY": "http://127.0.0.1:9",
        "HTTP_PROXY": "http://127.0.0.1:9",
        "NO_PROXY": "127.0.0.1",
    }
    env.pop("HF_TOKEN", None)
    if token:
        env["HF_TOKEN"] = token
    return subprocess.run(
        [sys.executable, str(SCRIPT), "cc_news", "--mirror", "--datasets-root", str(root), *extra],
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )


def descriptor(root: Path) -> dict[str, Any]:
    return json.loads((root / "cc-news" / "raw" / "datapackage.json").read_text())


def test_mirror_writes_the_pinned_shards_and_a_raw_descriptor(
    hub: tuple[FakeHub, str], tmp_path: Path
) -> None:
    _, endpoint = hub

    result = fetch(endpoint, tmp_path)

    assert result.returncode == 0, result.stderr
    raw = tmp_path / "cc-news" / "raw"
    assert (raw / "train-00000-of-00002.parquet").read_bytes() == SHARD_ZERO
    assert (raw / "train-00001-of-00002.parquet").read_bytes() == SHARD_ONE
    assert not (raw / "notes.txt").exists()
    assert [path.name for path in (tmp_path / "cc-news").iterdir()] == ["raw"]

    package = descriptor(tmp_path)
    assert package["$schema"] == "https://datapackage.org/profiles/2.0/datapackage.json"
    assert package["version"] == COMMIT
    [source] = package["sources"]
    assert source["version"] == COMMIT
    assert REPO in source["path"]
    resources = {resource["path"]: resource for resource in package["resources"]}
    assert resources.keys() == {"train-00000-of-00002.parquet", "train-00001-of-00002.parquet"}
    zero = resources["train-00000-of-00002.parquet"]
    assert (zero["hash"], zero["bytes"], zero["format"]) == (SHARD_ZERO_HASH, 15, "parquet")
    one = resources["train-00001-of-00002.parquet"]
    assert (one["hash"], one["bytes"]) == (SHARD_ONE_HASH, 14)


def test_rerun_keeps_matching_shards_and_files_it_does_not_own(
    hub: tuple[FakeHub, str], tmp_path: Path
) -> None:
    _, endpoint = hub
    raw = tmp_path / "cc-news" / "raw"
    raw.mkdir(parents=True)
    (raw / "mirror.meta.json").write_text("notes\n")
    (raw / "train-00000-of-00002.parquet").write_bytes(SHARD_ZERO)

    first = fetch(endpoint, tmp_path)
    first_descriptor = (raw / "datapackage.json").read_bytes()
    second = fetch(endpoint, tmp_path)

    assert first.returncode == 0, first.stderr
    assert second.returncode == 0, second.stderr
    assert (raw / "datapackage.json").read_bytes() == first_descriptor
    assert (raw / "mirror.meta.json").read_text() == "notes\n"
    assert (raw / "train-00001-of-00002.parquet").read_bytes() == SHARD_ONE


def test_a_differing_raw_file_is_refused_without_force(
    hub: tuple[FakeHub, str], tmp_path: Path
) -> None:
    _, endpoint = hub
    raw = tmp_path / "cc-news" / "raw"
    raw.mkdir(parents=True)
    (raw / "train-00000-of-00002.parquet").write_bytes(b"an older crawl")

    result = fetch(endpoint, tmp_path)

    assert result.returncode != 0
    assert "--force" in result.stderr
    assert (raw / "train-00000-of-00002.parquet").read_bytes() == b"an older crawl"
    assert not (raw / "datapackage.json").exists()


def test_force_clears_raw_but_not_the_dataset_folder(
    hub: tuple[FakeHub, str], tmp_path: Path
) -> None:
    _, endpoint = hub
    dataset = tmp_path / "cc-news"
    (dataset / "raw").mkdir(parents=True)
    (dataset / "raw" / "train-00000-of-00002.parquet").write_bytes(b"an older crawl")
    (dataset / "raw" / "stale.parquet").write_bytes(b"stale")
    (dataset / "dataset-canonical.jsonl").write_text("{}\n")

    result = fetch(endpoint, tmp_path, "--force")

    assert result.returncode == 0, result.stderr
    assert (dataset / "raw" / "train-00000-of-00002.parquet").read_bytes() == SHARD_ZERO
    assert not (dataset / "raw" / "stale.parquet").exists()
    assert (dataset / "dataset-canonical.jsonl").read_text() == "{}\n"


def test_bytes_that_disagree_with_the_hub_hash_are_never_kept(
    hub: tuple[FakeHub, str], tmp_path: Path
) -> None:
    fake, endpoint = hub
    fake.served["plain_text/train-00000-of-00002.parquet"] = b"PAR1 shard 0ero"

    result = fetch(endpoint, tmp_path)

    assert result.returncode != 0
    raw = tmp_path / "cc-news" / "raw"
    leftovers = sorted(path.name for path in raw.iterdir()) if raw.exists() else []
    assert "train-00000-of-00002.parquet" not in leftovers
    assert not [name for name in leftovers if name.endswith(".part")]
    assert "datapackage.json" not in leftovers


def test_a_raw_folder_from_another_commit_is_refused_before_any_download(
    hub: tuple[FakeHub, str], tmp_path: Path
) -> None:
    _, endpoint = hub
    raw = tmp_path / "cc-news" / "raw"
    raw.mkdir(parents=True)
    older = json.dumps({"sources": [{"version": "0" * 40}], "resources": []})
    (raw / "datapackage.json").write_text(older)

    result = fetch(endpoint, tmp_path)

    assert result.returncode != 0
    assert (raw / "datapackage.json").read_text() == older
    assert not (raw / "train-00000-of-00002.parquet").exists()


def test_the_token_reaches_the_hub_but_never_the_host_a_download_redirects_to(
    tmp_path: Path,
) -> None:
    fake = FakeHub()
    for cdn in serve(lambda path: fake.content(path.removeprefix("/"))):
        fake.cdn = cdn.endpoint
        for hub_server in serve(fake.respond):
            fake.endpoint = hub_server.endpoint

            result = fetch(hub_server.endpoint, tmp_path, token="hf_fixture")

            assert result.returncode == 0, result.stderr
            assert "Bearer hf_fixture" in hub_server.authorizations
            assert cdn.authorizations, "no download was redirected"
            assert all(authorization is None for authorization in cdn.authorizations)
