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
"""Generate Qwen3 embeddings for a canonical JSONL corpus.

  uv run scripts/embed_corpus.py <corpus.jsonl[.gz]> [--limit N]
  uv run scripts/embed_corpus.py <corpus.jsonl[.gz]> --merge-only

Writes a Parquet of (id, embedding) that `datapipe load embeddings` reads
directly. Runs on CUDA, MPS or CPU, and accepts plain or gzipped JSONL.

The corpus is streamed and checkpointed every --shard-size documents, so an
interrupted run resumes when re-invoked with the same arguments. Checkpoints
have to outlive the process: on an ephemeral disk, point --checkpoint-dir at
persistent storage.

Doubles as a Kaggle script kernel. Kernels run with no argv, so omitting the
corpus argument makes the script look for one under /kaggle/input and write to
/kaggle/working. Pass a corpus path and none of that applies.
"""

import argparse
import gzip
import json
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

import numpy as np
import pyarrow as pa
import pyarrow.parquet as pq
import torch
import torch.nn.functional as F
from transformers import AutoModel, AutoTokenizer

MODEL_ID = "Qwen/Qwen3-Embedding-0.6B"
MODEL_NAME = "qwen3-embedding:0.6b"  # stored as article_embeddings.model_name; must match query time

KAGGLE_INPUT = Path("/kaggle/input")
KAGGLE_WORKING = Path("/kaggle/working")

SWEEP_BATCH_SIZES = (64, 256, 512)
SWEEP_DOCS = 2000


def build_text(article: dict, full_content: bool) -> str:
    parts = [article.get("title") or "", article.get("description") or ""]
    if full_content:
        parts.append(article.get("full_content") or article.get("content") or "")
    return " ".join(p.strip() for p in parts if p and p.strip())


def open_text(path: Path):
    if path.suffix == ".gz":
        return gzip.open(path, "rt", encoding="utf-8")
    return open(path, encoding="utf-8")


def count_lines(path: Path) -> int:
    opener = gzip.open if path.suffix == ".gz" else open
    total = 0
    with opener(path, "rb") as fh:
        while chunk := fh.read(8 << 20):
            total += chunk.count(b"\n")
    return total


def iter_shards(path: Path, shard_size: int, full_content: bool, limit: int | None, is_done):
    """Yield (shard_idx, ids, texts) in file order.

    Shards already on disk are counted but not parsed, so a resumed run skips the
    finished prefix at read speed.
    """
    ids: list[str] = []
    texts: list[str] = []
    shard_idx = 0
    in_shard = 0
    seen = 0
    skip = is_done(0)

    with open_text(path) as fh:
        for lineno, line in enumerate(fh, 1):
            if not line.strip():
                continue
            if limit is not None and seen >= limit:
                break
            seen += 1

            if not skip:
                record = json.loads(line)
                if "id" not in record:
                    raise ValueError(f"{path}:{lineno} has no 'id' field")
                ids.append(str(record["id"]))
                texts.append(build_text(record, full_content))

            in_shard += 1
            if in_shard == shard_size:
                if not skip:
                    yield shard_idx, ids, texts
                ids, texts = [], []
                in_shard = 0
                shard_idx += 1
                skip = is_done(shard_idx)

    if in_shard and not skip:
        yield shard_idx, ids, texts


def pick_device(requested: str) -> torch.device:
    if requested != "auto":
        return torch.device(requested)
    if torch.cuda.is_available():
        return torch.device("cuda")
    if torch.backends.mps.is_available():
        return torch.device("mps")
    return torch.device("cpu")


def last_token_pool(hidden_states: torch.Tensor, attention_mask: torch.Tensor) -> torch.Tensor:
    """Qwen3-Embedding pools the last token, and tolerates either padding side."""
    left_padding = attention_mask[:, -1].sum() == attention_mask.shape[0]
    if left_padding:
        return hidden_states[:, -1]
    seq_len = attention_mask.sum(dim=1) - 1
    batch_idx = torch.arange(hidden_states.size(0), device=hidden_states.device)
    return hidden_states[batch_idx, seq_len]


def load_model(device: torch.device):
    dtype = torch.float32 if device.type == "cpu" else torch.float16
    tokenizer = AutoTokenizer.from_pretrained(MODEL_ID, trust_remote_code=True)
    model = AutoModel.from_pretrained(MODEL_ID, trust_remote_code=True, torch_dtype=dtype)
    model = model.to(device).eval()
    return tokenizer, model


@torch.inference_mode()
def embed_texts(texts, tokenizer, model, device, batch_size, max_length) -> np.ndarray:
    out = np.empty((len(texts), model.config.hidden_size), dtype=np.float32)

    # Batches pad to their longest member, so grouping similar lengths cuts wasted
    # compute. Undone below, keeping a shard's contents fixed by file order.
    order = sorted(range(len(texts)), key=lambda i: len(texts[i]))

    for lo in range(0, len(order), batch_size):
        idx = order[lo : lo + batch_size]
        encoded = tokenizer(
            [texts[i] for i in idx],
            padding=True,
            truncation=True,
            max_length=max_length,
            return_tensors="pt",
        ).to(device)
        hidden = model(**encoded).last_hidden_state
        vectors = last_token_pool(hidden, encoded["attention_mask"])
        vectors = F.normalize(vectors, p=2, dim=1)
        out[idx] = vectors.cpu().float().numpy()
    return out


def head_texts(path: Path, count: int, full_content: bool) -> list[str]:
    for _, _, texts in iter_shards(path, count, full_content, count, lambda i: False):
        return texts
    return []


def sweep_batch_size(texts, tokenizer, model, device, max_length) -> int:
    """Time a slice at each candidate size and return the fastest.

    The default suits Apple Silicon and is usually wrong for a discrete GPU, which
    is where this runs without argv to correct it.
    """
    best, best_rate = SWEEP_BATCH_SIZES[0], 0.0
    for size in SWEEP_BATCH_SIZES:
        started = time.time()
        try:
            embed_texts(texts, tokenizer, model, device, size, max_length)
        except (RuntimeError, torch.OutOfMemoryError) as err:
            print(f"batch {size:>4}: {type(err).__name__}, skipping")
            continue
        finally:
            if device.type == "cuda":
                torch.cuda.empty_cache()

        rate = len(texts) / (time.time() - started)
        print(f"batch {size:>4}: {rate:>6.1f} docs/s")
        if rate > best_rate:
            best, best_rate = size, rate
    return best


def shard_path(ckpt_dir: Path, idx: int) -> Path:
    return ckpt_dir / f"shard_{idx:05d}.npz"


def save_shard(ckpt_dir: Path, idx: int, ids: list[str], vectors: np.ndarray) -> None:
    ckpt_dir.mkdir(parents=True, exist_ok=True)
    tmp = shard_path(ckpt_dir, idx).with_suffix(".npz.tmp")
    # Uncompressed: float32 vectors barely compress, and this write blocks every shard.
    with open(tmp, "wb") as fh:
        np.savez(fh, ids=np.array(ids), embeddings=vectors)
    tmp.rename(shard_path(ckpt_dir, idx))


def existing_shards(ckpt_dir: Path) -> list[int]:
    if not ckpt_dir.is_dir():
        return []
    return sorted(int(p.stem.split("_")[1]) for p in ckpt_dir.glob("shard_*.npz"))


def list_column(vectors: np.ndarray) -> pa.ListArray:
    rows, dim = vectors.shape
    flat = pa.array(vectors.reshape(-1), type=pa.float32())
    offsets = pa.array(np.arange(0, rows * dim + 1, dim, dtype=np.int32))
    return pa.ListArray.from_arrays(offsets, flat)


def merge(ckpt_dir: Path, out_path: Path, max_length: int, full_content: bool) -> None:
    """Append every shard to one Parquet file, one row group per shard."""
    shards = existing_shards(ckpt_dir)
    if not shards:
        sys.exit(f"no checkpoints in {ckpt_dir}")
    if shards != list(range(len(shards))):
        missing = sorted(set(range(shards[-1] + 1)) - set(shards))
        sys.exit(f"checkpoints are not contiguous, missing shards: {missing}")

    # Row count has to be known before the writer opens, but only the first and last
    # shard need reading: every other shard is full.
    last = np.load(shard_path(ckpt_dir, shards[-1]), allow_pickle=False)
    dim = int(last["embeddings"].shape[1])
    rows = 0
    if len(shards) > 1:
        first = np.load(shard_path(ckpt_dir, 0), allow_pickle=False)
        rows = int(first["embeddings"].shape[0]) * (len(shards) - 1)
    rows += int(last["embeddings"].shape[0])

    schema = pa.schema(
        [pa.field("id", pa.string()), pa.field("embedding", pa.list_(pa.float32()))]
    ).with_metadata(
        {
            "model": MODEL_NAME,
            "hf_model_id": MODEL_ID,
            "dim": str(dim),
            "pooling": "last_token",
            "normalized": "l2",
            "max_length": str(max_length),
            "embed_full_content": str(full_content),
            "row_count": str(rows),
            "created_at": datetime.now(timezone.utc).isoformat(),
        }
    )

    out_path.parent.mkdir(parents=True, exist_ok=True)
    print(f"merging {len(shards)} shards -> {out_path}  ({rows:,} rows x {dim} dims)")
    with pq.ParquetWriter(out_path, schema, compression="zstd") as writer:
        for idx in shards:
            shard = np.load(shard_path(ckpt_dir, idx), allow_pickle=False)
            table = pa.Table.from_arrays(
                [
                    pa.array(shard["ids"].astype(str), type=pa.string()),
                    list_column(shard["embeddings"].astype(np.float32)),
                ],
                schema=schema,
            )
            writer.write_table(table)
            print(f"  shard {idx + 1}/{len(shards)}", end="\r", flush=True)

    print(f"\nwrote {out_path.stat().st_size / 1e6:.1f} MB")


def verify(out_path: Path) -> None:
    handle = pq.ParquetFile(out_path)
    print("\n=== verification ===")
    print(f"rows     : {handle.metadata.num_rows:,}")
    print(f"metadata : {handle.schema_arrow.metadata}")

    head = handle.read_row_group(0)
    sample = np.array(head.column("embedding").slice(0, 1000).to_pylist(), dtype=np.float32)
    norms = np.linalg.norm(sample, axis=1)
    print(
        f"l2 norms : mean={norms.mean():.4f} std={norms.std():.6f} "
        f"min={norms.min():.4f} max={norms.max():.4f}"
    )
    print(f"cos(0,1) : {float(np.dot(sample[0], sample[1])):.4f}")


def kaggle_corpus() -> Path | None:
    if not KAGGLE_INPUT.is_dir():
        return None
    for pattern in ("**/*.jsonl.gz", "**/*.jsonl"):
        matches = sorted(KAGGLE_INPUT.glob(pattern))
        if matches:
            return matches[0]
    return None


def apply_kaggle_defaults(args: argparse.Namespace) -> None:
    """Fill in what a kernel cannot pass on the command line."""
    corpus = kaggle_corpus()
    if corpus is None:
        sys.exit("no corpus argument, and no .jsonl or .jsonl.gz under /kaggle/input")

    args.input = corpus
    args.out = args.out or KAGGLE_WORKING / "embeddings.parquet"
    args.checkpoint_dir = args.checkpoint_dir or KAGGLE_WORKING / "checkpoints"
    args.clean_checkpoints = True  # kernel disk holds the shards and the Parquet at once
    args.auto_batch_size = True


def remove_checkpoints(ckpt_dir: Path) -> int:
    freed = sum(shard_path(ckpt_dir, i).stat().st_size for i in existing_shards(ckpt_dir))
    for idx in existing_shards(ckpt_dir):
        shard_path(ckpt_dir, idx).unlink()
    try:
        ckpt_dir.rmdir()
    except OSError:
        pass  # a crashed run can leave a .npz.tmp; the shards are what matter
    return freed


def fmt_duration(seconds: float) -> str:
    if seconds == float("inf"):
        return "?"
    m, s = divmod(int(seconds), 60)
    h, m = divmod(m, 60)
    return f"{h}h{m:02d}m" if h else f"{m}m{s:02d}s"


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("input", type=Path, nargs="?", help="canonical JSONL, optionally gzipped; omit under /kaggle/input")
    p.add_argument("-o", "--out", type=Path, help="output Parquet (default: <input dir>/embeddings.parquet)")
    p.add_argument("--checkpoint-dir", type=Path, help="default: <out dir>/.embed-checkpoints")
    # 64 measured fastest on Apple Silicon, where wide batches thrash unified memory.
    p.add_argument("--batch-size", type=int, default=64, help="documents per forward pass (lower if OOM)")
    p.add_argument(
        "--auto-batch-size",
        action="store_true",
        help=f"time {SWEEP_DOCS} docs at {', '.join(map(str, SWEEP_BATCH_SIZES))} and use the fastest",
    )
    p.add_argument("--shard-size", type=int, default=20000, help="documents per checkpoint file; also the resume granularity")
    p.add_argument("--max-length", type=int, default=512, help="tokens per document")
    p.add_argument("--full-content", action="store_true", help="also embed the article body (slower, more VRAM)")
    p.add_argument("--limit", type=int, help="stop after N documents; useful for timing a slice")
    p.add_argument("--device", default="auto", choices=["auto", "mps", "cuda", "cpu"])
    p.add_argument("--merge-only", action="store_true", help="skip embedding, just merge existing checkpoints")
    p.add_argument(
        "--clean-checkpoints",
        action="store_true",
        help="delete the shard dir after a successful merge; halves peak disk, forfeits resume",
    )
    p.add_argument("--force", action="store_true", help="overwrite an output Parquet this checkpoint dir did not produce")
    return p.parse_args()


def main() -> None:
    args = parse_args()
    if args.input is None:
        apply_kaggle_defaults(args)
    if not args.input.is_file():
        sys.exit(f"no such file: {args.input}")

    out_path = args.out or args.input.parent / "embeddings.parquet"
    ckpt_dir = args.checkpoint_dir or out_path.parent / ".embed-checkpoints"

    # Refuse only when no checkpoints exist, so a resumed run re-merges freely while a
    # stray path cannot clobber vectors this run did not produce.
    if out_path.exists() and not existing_shards(ckpt_dir) and not args.force:
        # A completed --clean-checkpoints run leaves this same state, and re-running is
        # how a dropped session resumes, so it has to no-op instead of failing.
        if args.clean_checkpoints:
            print(f"{out_path} is already complete; pass --force to regenerate")
            return
        sys.exit(f"{out_path} exists and {ckpt_dir} has no checkpoints; pass --force to overwrite")

    if not args.merge_only:
        total_docs = count_lines(args.input)
        if args.limit:
            total_docs = min(total_docs, args.limit)
        total_shards = -(-total_docs // args.shard_size)
        done = set(existing_shards(ckpt_dir))

        device = pick_device(args.device)
        print(f"input    : {args.input}  ({total_docs:,} docs)")
        print(f"device   : {device.type}")
        print(f"shards   : {total_shards} x {args.shard_size} docs  ({len(done)} already on disk)")
        print(f"ckpt dir : {ckpt_dir}")
        print(f"output   : {out_path}\n")

        tokenizer, model = load_model(device)
        print(f"model loaded, embedding dim {model.config.hidden_size}\n")

        batch_size = args.batch_size
        if args.auto_batch_size:
            batch_size = sweep_batch_size(
                head_texts(args.input, SWEEP_DOCS, args.full_content),
                tokenizer, model, device, args.max_length,
            )
            print(f"using --batch-size {batch_size}\n")

        started = time.time()
        embedded = 0
        for shard_idx, ids, texts in iter_shards(
            args.input, args.shard_size, args.full_content, args.limit, lambda i: i in done
        ):
            vectors = embed_texts(texts, tokenizer, model, device, batch_size, args.max_length)
            save_shard(ckpt_dir, shard_idx, ids, vectors)

            embedded += len(ids)
            elapsed = time.time() - started
            rate = embedded / elapsed if elapsed else 0
            remaining = (total_shards - shard_idx - 1) * args.shard_size
            eta = remaining / rate if rate else float("inf")
            print(
                f"shard {shard_idx + 1:>4}/{total_shards}  "
                f"{embedded:>8,} docs  {rate:>6.1f} docs/s  "
                f"elapsed {fmt_duration(elapsed)}  eta {fmt_duration(eta)}"
            )

        if embedded:
            print(f"\nembedded {embedded:,} docs in {fmt_duration(time.time() - started)}")
        else:
            print("nothing to do: every shard already checkpointed")

    merge(ckpt_dir, out_path, args.max_length, args.full_content)
    verify(out_path)

    if args.clean_checkpoints:
        freed = remove_checkpoints(ckpt_dir)
        size = f"{freed / 1e9:.2f} GB" if freed >= 1e9 else f"{freed / 1e6:.0f} MB"
        print(f"removed checkpoints, freed {size}")

    print(f"\nEMBEDDING_SOURCE=file EMBEDDING_FILE_PATH={out_path}")


if __name__ == "__main__":
    main()
