"""Draw the fixed-seed article samples from the cc-news canonical file.

Sample B holds 50,000 articles drawn uniformly without replacement; sample A is the first
10,000 of that draw, so A is a subset of B. Both are written in source line order.
"""

import hashlib
import json
import random
import sys
from pathlib import Path

SEED = 20261008
SAMPLE_B = 50_000
SAMPLE_A = 10_000
FIELDS = ("title", "description", "content")


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def main(source: Path, out_dir: Path) -> None:
    with source.open("rb") as handle:
        total = sum(1 for _ in handle)

    draw = random.Random(SEED).sample(range(total), SAMPLE_B)
    lines_b = set(draw)
    lines_a = set(draw[:SAMPLE_A])

    rows = {"a": [], "b": []}
    with source.open(encoding="utf-8") as handle:
        for line_no, line in enumerate(handle):
            if line_no not in lines_b:
                continue
            article = json.loads(line)
            row = {"line": line_no, "id": article["id"]}
            row.update({field: article.get(field) or "" for field in FIELDS})
            rows["b"].append(row)
            if line_no in lines_a:
                rows["a"].append(row)

    manifest = {
        "source": str(source),
        "source_sha256": sha256(source),
        "source_lines": total,
        "seed": SEED,
        "fields": FIELDS,
    }
    for name, sample in rows.items():
        path = out_dir / f"sample_{name}.jsonl"
        with path.open("w", encoding="utf-8") as handle:
            for row in sample:
                handle.write(json.dumps(row, ensure_ascii=False) + "\n")
        manifest[f"sample_{name}"] = {"rows": len(sample), "sha256": sha256(path)}

    (out_dir / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(json.dumps(manifest, indent=2))


if __name__ == "__main__":
    main(Path(sys.argv[1]), Path(sys.argv[2]))
