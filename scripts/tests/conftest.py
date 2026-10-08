import hashlib
import json
import os
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

import pyarrow as pa
import pyarrow.parquet as pq
import pytest
import torch
from tokenizers import Tokenizer, models, pre_tokenizers, processors
from transformers.models.qwen3 import Qwen3Config, Qwen3Model
from transformers.tokenization_utils_fast import PreTrainedTokenizerFast

SCRIPTS = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SCRIPTS))
TINY_MODEL_NAME = "tiny-qwen3"
TINY_REVISION = "test-rev"
EOS = "<|endoftext|>"


@dataclass(frozen=True)
class CanonicalPackage:
    descriptor: Path
    resource: Path


def article(
    article_id: str, title: str, description: str = "", content: str = "body"
) -> dict:
    return {
        "id": article_id,
        "url": f"https://example.com/{article_id}",
        "title": title,
        "description": description,
        "content": content,
        "language": "eng",
    }


def sha256_of(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def write_canonical_package(
    directory: Path, rows: list[dict], file_format: str = "parquet"
) -> CanonicalPackage:
    directory.mkdir(parents=True, exist_ok=True)
    resource = directory / f"articles.{file_format}"
    if file_format == "parquet":
        pq.write_table(pa.Table.from_pylist(rows), resource)
    else:
        resource.write_text(
            "".join(json.dumps(row) + "\n" for row in rows), encoding="utf-8"
        )

    descriptor = directory / "datapackage.json"
    descriptor.write_text(
        json.dumps(
            {
                "$schema": "https://datapackage.org/profiles/2.0/datapackage.json",
                "name": "cc-news",
                "version": "article-v2-0badcafe",
                "articleSchema": "article/v2",
                "language": "eng",
                "resources": [
                    {
                        "name": "articles",
                        "path": resource.name,
                        "format": file_format,
                        "bytes": resource.stat().st_size,
                        "hash": sha256_of(resource),
                    }
                ],
            }
        ),
        encoding="utf-8",
    )
    return CanonicalPackage(descriptor=descriptor, resource=resource)


@pytest.fixture(scope="session")
def tiny_model(tmp_path_factory: pytest.TempPathFactory) -> Path:
    """A random one-layer Qwen3 at the real model's 1024 width, with a per-character vocabulary.

    Every printable ASCII character and newline has its own id, so any byte that differs
    between two inputs reaches the model. Like the real tokenizer, it appends EOS.
    """
    directory = tmp_path_factory.mktemp("tiny-qwen3")
    vocabulary = [EOS, "[UNK]", "\n", *(chr(code) for code in range(32, 127))]
    tokenizer = Tokenizer(
        models.WordLevel(
            vocab={token: i for i, token in enumerate(vocabulary)}, unk_token="[UNK]"
        )
    )
    # The tokenizers stub types these setters as read-only properties.
    tokenizer.pre_tokenizer = pre_tokenizers.Split("", behavior="isolated")  # pyright: ignore[reportAttributeAccessIssue]
    tokenizer.post_processor = processors.TemplateProcessing(  # pyright: ignore[reportAttributeAccessIssue]
        single=f"$A {EOS}", pair=f"$A $B {EOS}", special_tokens=[(EOS, 0)]
    )
    PreTrainedTokenizerFast(
        tokenizer_object=tokenizer,
        pad_token=EOS,
        eos_token=EOS,
        unk_token="[UNK]",
        padding_side="left",
    ).save_pretrained(directory)

    torch.manual_seed(7)
    config = Qwen3Config(
        vocab_size=len(vocabulary),
        hidden_size=1024,
        intermediate_size=64,
        num_hidden_layers=1,
        num_attention_heads=8,
        num_key_value_heads=2,
        head_dim=128,
        max_position_embeddings=1024,
        use_sliding_window=False,
        sliding_window=None,  # pyright: ignore[reportArgumentType]  # None disables the window
    )
    Qwen3Model(config).save_pretrained(directory)
    return directory


@pytest.fixture
def tiny_model_args(tiny_model: Path) -> list[str]:
    return [
        "--model",
        str(tiny_model),
        "--revision",
        TINY_REVISION,
        "--model-name",
        TINY_MODEL_NAME,
        "--device",
        "cpu",
    ]


def run_script(script: str, *args: str | Path) -> subprocess.CompletedProcess[str]:
    env = {**os.environ, "HF_HUB_OFFLINE": "1", "TRANSFORMERS_OFFLINE": "1"}
    return subprocess.run(
        [sys.executable, str(SCRIPTS / script), *map(str, args)],
        capture_output=True,
        text=True,
        env=env,
        check=False,
    )
