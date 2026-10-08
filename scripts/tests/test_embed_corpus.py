import json
from pathlib import Path

import numpy as np
import pyarrow.parquet as pq
import pytest
from conftest import article, run_script, sha256_of, write_canonical_package

ROWS = [
    article(
        "6f1c2b8e-0d5a-4a57-9a0e-1b2c3d4e5f60",
        "Markets rally",
        "Stocks rose on Monday.",
    ),
    article("0a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d", "Storm hits coast"),
    article(
        "d4c3b2a1-9f8e-4d7c-b6a5-0f1e2d3c4b5a",
        "Election results",
        "Polls closed at eight.",
    ),
]


def embed(canonical: Path, output_root: Path, *args: str | Path):
    return run_script("embed_corpus.py", canonical, "--output-root", output_root, *args)


def only_package(output_root: Path) -> Path:
    (descriptor,) = output_root.glob("embeddings/*/*/datapackage.json")
    return descriptor


def test_refuses_a_canonical_resource_whose_bytes_do_not_match_its_hash(
    tmp_path, tiny_model_args
):
    canonical = write_canonical_package(tmp_path / "canonical", ROWS, "jsonl")
    tampered = canonical.resource.read_bytes().replace(b"Markets", b"Murkets")
    canonical.resource.write_bytes(tampered)

    result = embed(canonical.descriptor, tmp_path / "out", *tiny_model_args)

    assert result.returncode != 0
    assert "hash" in result.stderr
    assert not (tmp_path / "out").exists()


def read_embeddings(descriptor: Path) -> tuple[list[str], np.ndarray]:
    table = pq.read_table(descriptor.parent / "embeddings.parquet")
    return table.column("id").to_pylist(), np.array(
        table.column("embedding").to_pylist(), dtype=np.float32
    )


@pytest.mark.parametrize("file_format", ["parquet", "jsonl"])
def test_writes_one_vector_per_canonical_row_in_canonical_order(
    tmp_path, tiny_model_args, file_format
):
    canonical = write_canonical_package(tmp_path / "canonical", ROWS, file_format)

    result = embed(
        canonical.descriptor, tmp_path / "out", *tiny_model_args, "--shard-size", "2"
    )

    assert result.returncode == 0, result.stderr
    ids, _ = read_embeddings(only_package(tmp_path / "out"))
    assert ids == [
        "6f1c2b8e-0d5a-4a57-9a0e-1b2c3d4e5f60",
        "0a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d",
        "d4c3b2a1-9f8e-4d7c-b6a5-0f1e2d3c4b5a",
    ]


def test_lead_v1_embeds_the_stripped_title_and_description_joined_by_one_space_and_never_the_body(
    tmp_path, tiny_model_args
):
    rows = [
        article(
            "11111111-1111-4111-8111-111111111111",
            "  Storm hits coast ",
            " Winds at 90 km/h.\t",
            "first body",
        ),
        article(
            "22222222-2222-4222-8222-222222222222",
            "Storm hits coast Winds at 90 km/h.",
            "",
            "another body",
        ),
        article(
            "33333333-3333-4333-8333-333333333333",
            "Storm hits coast",
            "Winds at 90 km/h.",
            "first body",
        ),
    ]
    canonical = write_canonical_package(tmp_path / "canonical", rows, "jsonl")

    result = embed(
        canonical.descriptor, tmp_path / "out", *tiny_model_args, "--batch-size", "1"
    )

    assert result.returncode == 0, result.stderr
    _, vectors = read_embeddings(only_package(tmp_path / "out"))
    np.testing.assert_array_equal(vectors[0], vectors[1])
    np.testing.assert_array_equal(vectors[0], vectors[2])


def test_vectors_follow_the_text_they_embed(tmp_path, tiny_model_args):
    canonical = write_canonical_package(tmp_path / "canonical", ROWS)

    embed(canonical.descriptor, tmp_path / "out", *tiny_model_args)

    _, vectors = read_embeddings(only_package(tmp_path / "out"))
    assert not np.allclose(vectors[0], vectors[1])


def test_descriptor_records_the_model_recipe_and_canonical_source(
    tmp_path, tiny_model, tiny_model_args
):
    canonical = write_canonical_package(tmp_path / "canonical", ROWS)

    embed(canonical.descriptor, tmp_path / "out", *tiny_model_args)

    descriptor_path = only_package(tmp_path / "out")
    descriptor = json.loads(descriptor_path.read_text())
    assert descriptor["embedding"] == {
        "model": "tiny-qwen3",
        "hfModel": str(tiny_model),
        "revision": "test-rev",
        "tag": "tiny-qwen3@1024",
        "dim": 1024,
        "recipe": "lead-v1",
        "pooling": "last_token",
        "normalized": True,
        "maxTokens": 512,
    }
    assert descriptor["canonical"] == {
        "name": "cc-news",
        "version": "article-v2-0badcafe",
        "resourceHash": sha256_of(canonical.resource),
    }
    assert descriptor_path.parent.parent.name == "tiny-qwen3@1024"
    assert descriptor_path.parent.name == descriptor["version"]


def test_resource_is_described_by_its_real_size_and_hash(tmp_path, tiny_model_args):
    canonical = write_canonical_package(tmp_path / "canonical", ROWS)

    embed(canonical.descriptor, tmp_path / "out", *tiny_model_args)

    descriptor_path = only_package(tmp_path / "out")
    (resource,) = json.loads(descriptor_path.read_text())["resources"]
    written = descriptor_path.parent / "embeddings.parquet"
    assert resource["path"] == "embeddings.parquet"
    assert resource["format"] == "parquet"
    assert resource["bytes"] == written.stat().st_size
    assert resource["hash"] == sha256_of(written)


def test_version_hashes_the_carried_canonical_resource_hash_revision_recipe_dim_and_max_tokens(
    tmp_path, tiny_model
):
    projection = write_canonical_package(tmp_path / "slim", ROWS)
    descriptor = json.loads(projection.descriptor.read_text())
    descriptor["canonical"] = {
        "name": "cc-news",
        "version": "article-v2-0badcafe",
        "resourceHash": "sha256:" + "0" * 64,
    }
    projection.descriptor.write_text(json.dumps(descriptor))

    result = embed(
        projection.descriptor,
        tmp_path / "out",
        "--model",
        tiny_model,
        "--revision",
        "abc123",
        "--model-name",
        "tiny-qwen3",
        "--max-tokens",
        "512",
        "--device",
        "cpu",
    )

    assert result.returncode == 0, result.stderr
    written = json.loads(
        (
            tmp_path
            / "out/embeddings/tiny-qwen3@1024/lead-v1-1024-938b2254/datapackage.json"
        ).read_text()
    )
    assert written["version"] == "lead-v1-1024-938b2254"


def test_the_same_inputs_produce_byte_identical_packages(tmp_path, tiny_model_args):
    canonical = write_canonical_package(tmp_path / "canonical", ROWS)

    embed(
        canonical.descriptor, tmp_path / "first", *tiny_model_args, "--shard-size", "2"
    )
    embed(
        canonical.descriptor, tmp_path / "second", *tiny_model_args, "--shard-size", "2"
    )

    first, second = only_package(tmp_path / "first"), only_package(tmp_path / "second")
    assert first.read_bytes() == second.read_bytes()
    assert sha256_of(first.parent / "embeddings.parquet") == sha256_of(
        second.parent / "embeddings.parquet"
    )
