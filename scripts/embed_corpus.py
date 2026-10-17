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
"""Embed a canonical article package into an embeddings package.

  uv run scripts/embed_corpus.py datasets/cc-news/canonical/<version>/datapackage.json

Reads the package's single resource (parquet or jsonl, or the slim projection
slim_corpus.py writes), verifies its bytes and hash, and writes
embeddings/<tag-dir>/<version>/ under the dataset root, where the version is the
first 8 hex chars of the resource's sha256: embeddings.parquet with one
(id, embedding) row per canonical row in canonical order, and a datapackage.json
that records the model, recipe and canonical resource it came from.

Doubles as a Kaggle script kernel. Kernels run with no argv, so omitting the
package argument reads the one slim.datapackage.json under /kaggle/input and
writes to /kaggle/working.
"""

import argparse
import hashlib
import itertools
import json
import shutil
import sys
import time
import uuid
from collections.abc import Callable, Iterator
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Literal, NamedTuple

import numpy as np
import pyarrow as pa
import pyarrow.parquet as pq
import torch
import torch.nn.functional as F
from transformers.models.auto.modeling_auto import AutoModel
from transformers.models.auto.tokenization_auto import AutoTokenizer

QWEN_HF_MODEL = "Qwen/Qwen3-Embedding-0.6B"
QWEN_MODEL_NAME = "qwen3-embedding:0.6b"
QWEN_REVISION = "97b0c614be4d77ee51c0cef4e5f07c00f9eb65b3"

DOCUMENT_RECIPE = "lead-v1"
POOLING = "last_token"
DATAPACKAGE_PROFILE = "https://datapackage.org/profiles/2.0/datapackage.json"
PARQUET_MEDIATYPE = "application/vnd.apache.parquet"

KAGGLE_INPUT = Path("/kaggle/input")
# The kaggle CLI never uploads a file named datapackage.json, so the slim
# package that travels to Kaggle names its descriptor differently.
SLIM_DESCRIPTOR = "slim.datapackage.json"
KAGGLE_WORKING = Path("/kaggle/working")

CHECKPOINT_MANIFEST = "manifest.json"

SWEEP_BATCH_SIZES = (64, 256, 512)
SWEEP_DOCS = 2000

ResourceFormat = Literal["parquet", "jsonl"]


class PackageError(Exception):
    """A package, resource or run configuration that cannot be used as given."""


@dataclass(frozen=True, slots=True)
class CanonicalRef:
    name: str
    version: str
    resource_hash: str

    def to_descriptor(self) -> dict[str, str]:
        return {
            "name": self.name,
            "version": self.version,
            "resourceHash": self.resource_hash,
        }


@dataclass(frozen=True, slots=True)
class SourcePackage:
    """A canonical package, or a projection of one that names its canonical source."""

    resource: Path
    resource_format: ResourceFormat
    resource_hash: str
    canonical: CanonicalRef
    language: str | None


class LeadRow(NamedTuple):
    id: str
    text: str


@dataclass(frozen=True, slots=True)
class Shard:
    index: int
    ids: list[str]
    texts: list[str]


@dataclass(frozen=True, slots=True)
class Encoder:
    tokenizer: Any
    model: Any
    device: torch.device
    native_dim: int


@dataclass(frozen=True, slots=True)
class EmbeddingSpec:
    hf_model: str
    model_name: str
    revision: str
    recipe: str
    dim: int
    max_tokens: int

    @property
    def tag(self) -> str:
        return f"{self.model_name}@{self.dim}"

    def to_descriptor(self) -> dict[str, Any]:
        return {
            "model": self.model_name,
            "hfModel": self.hf_model,
            "revision": self.revision,
            "tag": self.tag,
            "dim": self.dim,
            "recipe": self.recipe,
            "pooling": POOLING,
            "normalized": True,
            "maxTokens": self.max_tokens,
        }


@dataclass(frozen=True, slots=True)
class Inference:
    """How a run embedded: these change output bytes slightly but are not version inputs."""

    batch_size: int
    dtype: str
    limit: int | None = None


def embedding_descriptor(spec: EmbeddingSpec, inference: Inference) -> dict[str, Any]:
    block = {
        **spec.to_descriptor(),
        "batchSize": inference.batch_size,
        "dtype": inference.dtype,
    }
    if inference.limit is not None:
        block["limit"] = inference.limit
    return block


def sha256_hash(path: Path) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as file:
        while chunk := file.read(8 << 20):
            digest.update(chunk)
    return f"sha256:{digest.hexdigest()}"


def describe_parquet_resource(path: Path, *, name: str) -> dict[str, Any]:
    return {
        "name": name,
        "path": path.name,
        "format": "parquet",
        "mediatype": PARQUET_MEDIATYPE,
        "bytes": path.stat().st_size,
        "hash": sha256_hash(path),
    }


def write_descriptor(
    directory: Path, descriptor: dict[str, Any], *, name: str = "datapackage.json"
) -> None:
    text = json.dumps(descriptor, indent=2, ensure_ascii=False) + "\n"
    (directory / name).write_text(text, encoding="utf-8")


def read_descriptor(descriptor_path: Path) -> dict[str, Any]:
    try:
        return json.loads(descriptor_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise PackageError(f"read descriptor {descriptor_path}: {error}") from error


def single_resource(
    descriptor_path: Path, descriptor: dict[str, Any]
) -> dict[str, Any]:
    resources = descriptor.get("resources") or []
    if len(resources) != 1:
        raise PackageError(
            f"{descriptor_path}: expected exactly one resource, found {len(resources)}"
        )
    return resources[0]


def open_package(descriptor_path: Path) -> SourcePackage:
    """Read a package descriptor and verify its single resource by size and hash."""
    descriptor = read_descriptor(descriptor_path)
    resource = single_resource(descriptor_path, descriptor)
    resource_format = resource.get("format")
    if resource_format not in ("parquet", "jsonl"):
        raise PackageError(
            f"{descriptor_path}: resource format {resource_format!r} is not parquet or jsonl"
        )

    resource_path = resolve_resource_path(descriptor_path, resource.get("path"))
    verify_resource(resource_path, resource)

    try:
        carried = descriptor.get("canonical")
        canonical = (
            CanonicalRef(carried["name"], carried["version"], carried["resourceHash"])
            if carried
            else CanonicalRef(
                descriptor["name"], descriptor["version"], resource["hash"]
            )
        )
    except KeyError as error:
        raise PackageError(
            f"{descriptor_path}: missing {error} for the canonical reference"
        ) from error
    return SourcePackage(
        resource=resource_path,
        resource_format=resource_format,
        resource_hash=resource["hash"],
        canonical=canonical,
        language=descriptor.get("language"),
    )


def resolve_resource_path(descriptor_path: Path, relative: object) -> Path:
    if not isinstance(relative, str) or not relative:
        raise PackageError(f"{descriptor_path}: resource has no path")
    path = Path(relative)
    if path.is_absolute() or ".." in path.parts:
        raise PackageError(
            f"{descriptor_path}: resource path {relative!r} leaves the package directory"
        )
    return descriptor_path.parent / path


def verify_resource(path: Path, resource: dict[str, Any]) -> None:
    if not path.is_file():
        raise PackageError(f"resource {path} does not exist")
    size = path.stat().st_size
    if size != resource.get("bytes"):
        raise PackageError(
            f"resource {path} is {size} bytes, descriptor says {resource.get('bytes')}"
        )
    actual = sha256_hash(path)
    if actual != resource.get("hash"):
        raise PackageError(
            f"resource {path} hash is {actual}, descriptor says {resource.get('hash')}"
        )


def lead_text(title: str | None, description: str | None) -> str:
    """Build the lead-v1 document text: the stripped, non-empty title and description."""
    parts = (part.strip() for part in (title or "", description or ""))
    return " ".join(part for part in parts if part)


def iter_lead_rows(source: SourcePackage, limit: int | None) -> Iterator[LeadRow]:
    """Stream (id, lead text) per row in resource order without holding the corpus in memory."""
    records = (
        iter_parquet_records(source.resource)
        if source.resource_format == "parquet"
        else iter_jsonl_records(source.resource)
    )
    for row_number, record in enumerate(itertools.islice(records, limit), 1):
        yield LeadRow(
            id=canonical_id(record.get("id"), row_number),
            text=lead_text(record.get("title"), record.get("description")),
        )


def iter_parquet_records(path: Path) -> Iterator[dict[str, Any]]:
    with pq.ParquetFile(path) as file:
        columns = [
            name
            for name in ("id", "title", "description")
            if name in file.schema_arrow.names
        ]
        for batch in file.iter_batches(columns=columns, batch_size=8192):
            yield from batch.to_pylist()


def iter_jsonl_records(path: Path) -> Iterator[dict[str, Any]]:
    with open(path, encoding="utf-8") as file:
        for line_number, line in enumerate(file, 1):
            if not line.strip():
                continue
            try:
                yield json.loads(line)
            except json.JSONDecodeError as error:
                raise PackageError(f"{path}:{line_number}: {error}") from error


def canonical_id(value: object, row_number: int) -> str:
    if not isinstance(value, str):
        raise PackageError(f"row {row_number} has no string id")
    try:
        parsed = uuid.UUID(value)
    except ValueError as error:
        raise PackageError(f"row {row_number} id {value!r} is not a uuid") from error
    # Loaders compare ids byte for byte, so a rewritten id would pair with nothing.
    if str(parsed) != value:
        raise PackageError(
            f"row {row_number} id {value!r} is not canonical lowercase uuid text"
        )
    return value


def scan_corpus(source: SourcePackage, limit: int | None) -> int:
    """Count the rows to embed, refusing any row whose lead text is empty.

    Loaders pair vectors with articles by position, so a row cannot be skipped.
    """
    rows = 0
    empty_ids: list[str] = []
    for row in iter_lead_rows(source, limit):
        rows += 1
        if not row.text:
            empty_ids.append(row.id)
    if empty_ids:
        raise PackageError(
            f"{len(empty_ids)} rows have no title or description, first ids: {empty_ids[:5]}"
        )
    if rows == 0:
        raise PackageError(f"{source.resource} has no rows")
    return rows


def iter_shards(
    source: SourcePackage, *, shard_size: int, limit: int | None, skip: set[int]
) -> Iterator[Shard]:
    """Cut the corpus into fixed shards in resource order, skipping finished ones.

    Shard N always covers the same rows, which is what makes resume exact.
    """
    ids: list[str] = []
    texts: list[str] = []
    index = 0
    for position, row in enumerate(iter_lead_rows(source, limit)):
        if index not in skip:
            ids.append(row.id)
            texts.append(row.text)
        if (position + 1) % shard_size == 0:
            if index not in skip:
                yield Shard(index, ids, texts)
            ids, texts = [], []
            index += 1
    if ids:
        yield Shard(index, ids, texts)


def pick_device(requested: str) -> torch.device:
    """Resolve `auto` to CUDA or CPU; Apple's MPS only when asked for by name."""
    if requested != "auto":
        return torch.device(requested)
    return torch.device("cuda" if torch.cuda.is_available() else "cpu")


def inference_dtype(device: torch.device) -> torch.dtype:
    return torch.float32 if device.type == "cpu" else torch.float16


def dtype_name(device: torch.device) -> str:
    return str(inference_dtype(device)).removeprefix("torch.")


def load_encoder(*, hf_model: str, revision: str, device: torch.device) -> Encoder:
    tokenizer = AutoTokenizer.from_pretrained(hf_model, revision=revision)
    model = AutoModel.from_pretrained(
        hf_model, revision=revision, torch_dtype=inference_dtype(device)
    )
    resolved = getattr(model.config, "_commit_hash", None)
    if resolved is not None and resolved != revision:
        raise PackageError(
            f"revision {revision!r} resolved to commit {resolved}; pass the commit sha"
        )
    return Encoder(
        tokenizer, model.to(device).eval(), device, int(model.config.hidden_size)
    )


def last_token_pool(
    hidden_states: torch.Tensor, attention_mask: torch.Tensor
) -> torch.Tensor:
    """Pool the last real token, whichever side the tokenizer pads on."""
    left_padding = attention_mask[:, -1].sum() == attention_mask.shape[0]
    if left_padding:
        return hidden_states[:, -1]
    lengths = attention_mask.sum(dim=1) - 1
    batch = torch.arange(hidden_states.size(0), device=hidden_states.device)
    return hidden_states[batch, lengths]


@torch.inference_mode()
def encode(
    encoder: Encoder, texts: list[str], *, batch_size: int, max_tokens: int, dim: int
) -> np.ndarray:
    """Embed texts, sliced to `dim` and L2-normalised, one row per text in input order."""
    vectors = np.empty((len(texts), dim), dtype=np.float32)

    # Batches pad to their longest member, so grouping similar lengths cuts wasted
    # compute. Writing by index undoes the sort.
    order = sorted(range(len(texts)), key=lambda i: len(texts[i]))
    for start in range(0, len(order), batch_size):
        batch = order[start : start + batch_size]
        encoded = encoder.tokenizer(
            [texts[i] for i in batch],
            padding=True,
            truncation=True,
            max_length=max_tokens,
            return_tensors="pt",
        ).to(encoder.device)
        hidden = encoder.model(**encoded).last_hidden_state
        pooled = last_token_pool(hidden, encoded["attention_mask"])[:, :dim]
        vectors[batch] = F.normalize(pooled.float(), p=2, dim=1).cpu().numpy()
    return vectors


def non_finite_ids(ids: list[str], vectors: np.ndarray) -> list[str]:
    """Return the ids whose vector holds a NaN or inf, which fp16 overflow produces silently."""
    broken_rows = np.flatnonzero(~np.isfinite(vectors).all(axis=1))
    return [ids[row] for row in broken_rows]


def sweep_batch_size(
    encoder: Encoder, texts: list[str], *, max_tokens: int, dim: int
) -> int:
    """Time a slice at each candidate size and return the fastest.

    The default suits Apple Silicon and is usually wrong for a discrete GPU, which
    is where this runs without argv to correct it.
    """
    best, best_rate = SWEEP_BATCH_SIZES[0], 0.0
    for size in SWEEP_BATCH_SIZES:
        started = time.monotonic()
        try:
            encode(encoder, texts, batch_size=size, max_tokens=max_tokens, dim=dim)
        except (RuntimeError, torch.OutOfMemoryError) as error:
            print(f"batch {size:>4}: {type(error).__name__}, skipping")
            continue
        finally:
            if encoder.device.type == "cuda":
                torch.cuda.empty_cache()
        rate = len(texts) / (time.monotonic() - started)
        print(f"batch {size:>4}: {rate:>6.1f} docs/s")
        if rate > best_rate:
            best, best_rate = size, rate
    return best


def shard_path(checkpoint_dir: Path, index: int) -> Path:
    return checkpoint_dir / f"shard_{index:05d}.npz"


def save_shard(checkpoint_dir: Path, shard: Shard, vectors: np.ndarray) -> None:
    checkpoint_dir.mkdir(parents=True, exist_ok=True)
    temporary = shard_path(checkpoint_dir, shard.index).with_suffix(".npz.tmp")
    # Uncompressed: float32 vectors barely compress, and this write blocks every shard.
    with open(temporary, "wb") as file:
        np.savez(file, ids=np.array(shard.ids), embeddings=vectors)
    temporary.rename(shard_path(checkpoint_dir, shard.index))


def claim_checkpoints(checkpoint_dir: Path, key: dict[str, Any]) -> None:
    """Bind a checkpoint dir to one run's settings, refusing shards written under others.

    The key holds every setting that changes a shard's bytes, so resuming can never
    splice vectors from two configurations, or a --limit trial, into one package.
    """
    manifest = checkpoint_dir / CHECKPOINT_MANIFEST
    if manifest.exists():
        recorded = read_descriptor(manifest)
        differing = sorted(
            name
            for name in key.keys() | recorded.keys()
            if key.get(name) != recorded.get(name)
        )
        if differing:
            details = ", ".join(
                f"{name} {recorded.get(name)!r} -> {key.get(name)!r}"
                for name in differing
            )
            raise PackageError(
                f"{checkpoint_dir} holds checkpoints from other settings ({details}); "
                "use another --checkpoint-dir or delete it"
            )
        return
    if finished_shards(checkpoint_dir):
        raise PackageError(
            f"{checkpoint_dir} holds shards with no {CHECKPOINT_MANIFEST}"
        )
    checkpoint_dir.mkdir(parents=True, exist_ok=True)
    temporary = manifest.with_suffix(".tmp")
    temporary.write_text(
        json.dumps(key, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    temporary.rename(manifest)


def settings_digest(run_key: dict[str, Any]) -> str:
    """Name a default checkpoint dir after the run's input and settings.

    Batch size and dtype stay out of the name, so a resumed --auto-batch-size run
    finds its shards, and the manifest refuses one with another batch size.
    """
    encoded = json.dumps(run_key, sort_keys=True).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()[:8]


def recorded_batch_size(checkpoint_dir: Path) -> int | None:
    manifest = checkpoint_dir / CHECKPOINT_MANIFEST
    if not manifest.exists():
        return None
    return read_descriptor(manifest).get("batchSize")


def finished_shards(checkpoint_dir: Path) -> list[int]:
    if not checkpoint_dir.is_dir():
        return []
    return sorted(
        int(path.stem.split("_")[1]) for path in checkpoint_dir.glob("shard_*.npz")
    )


def vector_column(vectors: np.ndarray) -> pa.ListArray:
    rows, dim = vectors.shape
    values = pa.array(vectors.reshape(-1), type=pa.float32())
    offsets = pa.array(np.arange(0, rows * dim + 1, dim, dtype=np.int32))
    return pa.ListArray.from_arrays(offsets, values)


EMBEDDINGS_SCHEMA = pa.schema(
    [pa.field("id", pa.string()), pa.field("embedding", pa.list_(pa.float32()))]
)


def merge_shards(checkpoint_dir: Path, *, shard_count: int, out_path: Path) -> None:
    """Stream every shard into one Parquet file, one row group per shard."""
    missing = sorted(set(range(shard_count)) - set(finished_shards(checkpoint_dir)))
    if missing:
        raise PackageError(f"{checkpoint_dir} is missing shards {missing}")
    with pq.ParquetWriter(out_path, EMBEDDINGS_SCHEMA, compression="zstd") as writer:
        for index in range(shard_count):
            with np.load(
                shard_path(checkpoint_dir, index), allow_pickle=False
            ) as shard:
                ids = pa.array(shard["ids"].astype(str), type=pa.string())
                table = pa.Table.from_arrays(
                    [ids, vector_column(shard["embeddings"])], schema=EMBEDDINGS_SCHEMA
                )
            writer.write_table(table)


def content_version(resource: dict[str, Any]) -> str:
    """Version a package by its resource: the first 8 hex chars of the resource's sha256."""
    return resource["hash"].removeprefix("sha256:")[:8]


def publish_version(parent: Path, build: Callable[[Path], str]) -> Path:
    """Build a package in a staging dir, then rename it to `<parent>/<version>/`.

    `build` fills the staging dir and returns the version its bytes give. A version
    folder that already exists holds the same bytes, so it is kept and the staging
    copy discarded.
    """
    parent.mkdir(parents=True, exist_ok=True)
    with staged(parent / ".staging") as staging:
        final_dir = parent / build(staging)
        if not final_dir.exists():
            staging.rename(final_dir)
    return final_dir


def publish_replacing(final_dir: Path, build: Callable[[Path], str]) -> None:
    """Build a package in a staging dir and swap it in at a fixed location."""
    final_dir.parent.mkdir(parents=True, exist_ok=True)
    with staged(final_dir.with_name(f".{final_dir.name}.staging")) as staging:
        build(staging)
        retired = final_dir.with_name(f".{final_dir.name}.old")
        shutil.rmtree(retired, ignore_errors=True)
        if final_dir.exists():
            final_dir.rename(retired)
        staging.rename(final_dir)
        shutil.rmtree(retired, ignore_errors=True)


@contextmanager
def staged(staging: Path) -> Iterator[Path]:
    """Yield a fresh staging dir and remove whatever is left of it afterwards, on any exit."""
    shutil.rmtree(staging, ignore_errors=True)
    staging.mkdir(parents=True)
    try:
        yield staging
    finally:
        shutil.rmtree(staging, ignore_errors=True)


def dataset_root_of(descriptor_path: Path) -> Path:
    """Return datasets/<ds> for a package at datasets/<ds>/canonical/<version>/datapackage.json."""
    version_dir = descriptor_path.resolve().parent
    if version_dir.parent.name != "canonical":
        raise PackageError(
            f"{descriptor_path} is not under <dataset>/canonical/<version>/; pass --output-root"
        )
    return version_dir.parent.parent


def positive_int(text: str) -> int:
    try:
        value = int(text)
    except ValueError as error:
        raise argparse.ArgumentTypeError(f"{text!r} is not an integer") from error
    if value < 1:
        raise argparse.ArgumentTypeError(f"{value} is below 1")
    return value


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    # No abbreviations: a stale flag such as --out must not quietly become --output-root.
    parser = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        allow_abbrev=False,
    )
    parser.add_argument(
        "package",
        nargs="?",
        type=Path,
        help="canonical datapackage.json or slim.datapackage.json",
    )
    parser.add_argument(
        "--output-root", type=Path, help="dataset root to write embeddings/ under"
    )
    parser.add_argument(
        "--checkpoint-dir",
        type=Path,
        help="default: embeddings/<tag-dir>/.checkpoints-<settings>",
    )
    parser.add_argument(
        "--model",
        default=QWEN_HF_MODEL,
        help="Hugging Face model id or local directory",
    )
    parser.add_argument(
        "--revision", help=f"model commit; required unless --model is {QWEN_HF_MODEL}"
    )
    parser.add_argument(
        "--model-name", help=f"tag stem; required unless --model is {QWEN_HF_MODEL}"
    )
    parser.add_argument(
        "--dim",
        type=positive_int,
        help="keep the first N dimensions, then L2-normalise (default: the model's width)",
    )
    parser.add_argument(
        "--max-tokens",
        type=positive_int,
        default=512,
        help="input tokens per document; the rest is cut",
    )
    # 64 measured fastest on Apple Silicon, where wide batches thrash unified memory.
    parser.add_argument(
        "--batch-size",
        type=positive_int,
        default=64,
        help="documents per forward pass (lower if OOM)",
    )
    parser.add_argument(
        "--auto-batch-size",
        action="store_true",
        help=f"time {SWEEP_DOCS} docs at {', '.join(map(str, SWEEP_BATCH_SIZES))} and use the fastest",
    )
    parser.add_argument(
        "--shard-size",
        type=positive_int,
        default=20000,
        help="documents per checkpoint; the resume unit",
    )
    parser.add_argument(
        "--limit",
        type=positive_int,
        help="embed only the first N rows, as a trial package marked by embedding.limit",
    )
    parser.add_argument(
        "--device",
        default="auto",
        choices=["auto", "cuda", "cpu", "mps"],
        help="auto never picks mps",
    )
    parser.add_argument(
        "--clean-checkpoints",
        action="store_true",
        help="delete the checkpoints once the package is written; halves what stays on disk, not the peak, and forfeits resume",
    )
    return parser.parse_args(argv)


def resolve_model_identity(args: argparse.Namespace) -> tuple[str, str]:
    """Return (revision, model name), defaulting both only for the pinned Qwen model."""
    if args.model == QWEN_HF_MODEL:
        return args.revision or QWEN_REVISION, args.model_name or QWEN_MODEL_NAME
    if not args.revision or not args.model_name:
        raise PackageError(
            "--revision and --model-name are required with a model other than the default"
        )
    return args.revision, args.model_name


def resolve_dim(requested: int | None, native_dim: int) -> int:
    if requested is None:
        return native_dim
    if not 1 <= requested <= native_dim:
        raise PackageError(
            f"--dim {requested} is outside 1..{native_dim}, the model's width"
        )
    return requested


def kaggle_package() -> Path:
    matches = (
        sorted(KAGGLE_INPUT.glob(f"**/{SLIM_DESCRIPTOR}"))
        if KAGGLE_INPUT.is_dir()
        else []
    )
    if len(matches) != 1:
        raise PackageError(
            f"no package argument, and {len(matches)} {SLIM_DESCRIPTOR} files under {KAGGLE_INPUT}"
        )
    return matches[0]


def apply_kaggle_defaults(args: argparse.Namespace) -> Path:
    """Fill in what a kernel cannot pass on the command line, and return the package."""
    args.output_root = args.output_root or KAGGLE_WORKING
    args.checkpoint_dir = args.checkpoint_dir or KAGGLE_WORKING / "checkpoints"
    # Kernel disk has to hold the shards and the Parquet at once.
    args.clean_checkpoints = True
    args.auto_batch_size = True
    return kaggle_package()


def run(args: argparse.Namespace) -> Path:
    package: Path = args.package or apply_kaggle_defaults(args)
    revision, model_name = resolve_model_identity(args)
    source = open_package(package)
    output_root = args.output_root or dataset_root_of(package)
    total_rows = scan_corpus(source, args.limit)

    device = pick_device(args.device)
    encoder = load_encoder(hf_model=args.model, revision=revision, device=device)
    spec = EmbeddingSpec(
        hf_model=args.model,
        model_name=model_name,
        revision=revision,
        recipe=DOCUMENT_RECIPE,
        dim=resolve_dim(args.dim, encoder.native_dim),
        max_tokens=args.max_tokens,
    )
    tag_dir = output_root / "embeddings" / spec.tag.replace(":", "-")
    run_key = {
        "resourceHash": source.resource_hash,
        "canonicalResourceHash": source.canonical.resource_hash,
        "shardSize": args.shard_size,
        "limit": args.limit,
        **spec.to_descriptor(),
    }
    checkpoint_dir = (
        args.checkpoint_dir or tag_dir / f".checkpoints-{settings_digest(run_key)}"
    )
    batch_size = args.batch_size
    if args.auto_batch_size:
        # A resumed run reuses the batch size its checkpoints recorded, so it
        # reproduces those bytes instead of whatever the timing sweep picks today.
        batch_size = recorded_batch_size(checkpoint_dir) or sweep_batch_size(
            encoder,
            next(
                iter_shards(source, shard_size=SWEEP_DOCS, limit=SWEEP_DOCS, skip=set())
            ).texts,
            max_tokens=spec.max_tokens,
            dim=spec.dim,
        )
        print(f"using --batch-size {batch_size}\n")
    inference = Inference(
        batch_size=batch_size, dtype=dtype_name(device), limit=args.limit
    )
    claim_checkpoints(
        checkpoint_dir, {**run_key, **embedding_descriptor(spec, inference)}
    )

    shard_count = -(-total_rows // args.shard_size)
    embed_shards(
        source,
        encoder,
        EmbedPlan(
            spec=spec,
            checkpoint_dir=checkpoint_dir,
            shard_size=args.shard_size,
            shard_count=shard_count,
            limit=args.limit,
            batch_size=batch_size,
        ),
    )

    def build(directory: Path) -> str:
        resource_path = directory / "embeddings.parquet"
        merge_shards(checkpoint_dir, shard_count=shard_count, out_path=resource_path)
        resource = describe_parquet_resource(resource_path, name="embeddings")
        descriptor: dict[str, Any] = {
            "$schema": DATAPACKAGE_PROFILE,
            "name": f"{source.canonical.name}-embeddings",
            "version": content_version(resource),
        }
        if source.language:
            descriptor["language"] = source.language
        descriptor["embedding"] = embedding_descriptor(spec, inference)
        descriptor["canonical"] = source.canonical.to_descriptor()
        descriptor["resources"] = [resource]
        write_descriptor(directory, descriptor)
        return descriptor["version"]

    final_dir = publish_version(tag_dir, build)
    print(f"wrote {final_dir}")

    if args.clean_checkpoints:
        shutil.rmtree(checkpoint_dir, ignore_errors=True)
    return final_dir


@dataclass(frozen=True, slots=True)
class EmbedPlan:
    spec: EmbeddingSpec
    checkpoint_dir: Path
    shard_size: int
    shard_count: int
    limit: int | None
    batch_size: int


def embed_shards(source: SourcePackage, encoder: Encoder, plan: EmbedPlan) -> None:
    """Embed and checkpoint every shard not already on disk."""
    done = set(finished_shards(plan.checkpoint_dir))
    print(f"input    : {source.resource}")
    print(f"device   : {encoder.device.type}")
    print(
        f"shards   : {plan.shard_count} x {plan.shard_size} rows  ({len(done)} on disk)"
    )
    print(f"ckpt dir : {plan.checkpoint_dir}\n")

    started = time.monotonic()
    embedded = 0
    shards = iter_shards(
        source, shard_size=plan.shard_size, limit=plan.limit, skip=done
    )
    for shard in shards:
        vectors = encode(
            encoder,
            shard.texts,
            batch_size=plan.batch_size,
            max_tokens=plan.spec.max_tokens,
            dim=plan.spec.dim,
        )
        broken = non_finite_ids(shard.ids, vectors)
        if broken:
            raise PackageError(
                f"shard {shard.index} has {len(broken)} non-finite vectors, first ids: {broken[:5]}"
            )
        save_shard(plan.checkpoint_dir, shard, vectors)
        embedded += len(shard.ids)
        rate = embedded / max(time.monotonic() - started, 1e-9)
        print(
            f"shard {shard.index + 1:>4}/{plan.shard_count}  {embedded:>8,} rows  {rate:>6.1f} rows/s"
        )


def main() -> None:
    try:
        run(parse_args())
    except PackageError as error:
        sys.exit(f"embed_corpus: {error}")


if __name__ == "__main__":
    main()
