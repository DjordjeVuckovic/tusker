import pytest
from embed_corpus import version

ZERO_HASH = "sha256:" + "0" * 64


@pytest.mark.parametrize(
    ("prefix", "inputs", "expected"),
    [
        ("article-v2", ("ab", "c"), "article-v2-601d5476"),
        ("article-v2", ("a", "bc"), "article-v2-3fafa1cf"),
        (
            "lead-v1-1024",
            (ZERO_HASH, "abc123", "lead-v1", "1024", "512"),
            "lead-v1-1024-938b2254",
        ),
    ],
    ids=["split-after-ab", "split-after-a", "embeddings-inputs"],
)
def test_version_matches_the_shared_test_vectors(prefix, inputs, expected):
    assert version(prefix, *inputs) == expected
