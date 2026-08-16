# CC-News

HuggingFace [vblagoje/cc_news](https://huggingface.co/datasets/vblagoje/cc_news), the
CommonCrawl News crawl with bodies extracted by
[news-please](https://github.com/fhamborg/news-please).

|          |                                                                               |
|----------|-------------------------------------------------------------------------------|
| Articles | 708,241                                                                       |
| Domains  | 8,759                                                                         |
| Period   | 2017-01 to 2019-08                                                            |
| Language | English                                                                       |
| Columns  | `title`, `text`, `description`, `url`, `date`, `domain` (+ unused `image_url`) |
| On HF    | 5 parquet shards, ~1.1 GB                                                     |

77,598 rows (11%) carry an empty `date`. The fetch script writes those as an empty
field, which `datapipe preprocess` skips since the target is optional. `domain` is a
hostname such as `www.pointemagazine.com`, while `all-the-news` supplies a publication
name for the same canonical field, so anything faceting across both corpora has to
account for the difference.

```
datasets/cc-news/
  raw/                       mirrored HuggingFace shards (--format raw)
  dataset-100k.csv           slice written by scripts/fetch_hf_dataset.py
  dataset-100k.meta.json     provenance: repo, mode, seed, row count
  canonical-dataset.jsonl    datapipe preprocess output
  embeddings.parquet         Colab embeddings (qwen3-embedding:0.6b)
```

Payloads are gitignored, so fetch or regenerate them.

## Fetch

```bash
uv run scripts/fetch_hf_dataset.py cc_news --format raw       # mirror the shards once
uv run scripts/fetch_hf_dataset.py cc_news 100000             # reservoir sample (default)
uv run scripts/fetch_hf_dataset.py cc_news 100000 500000      # nested slices from one scan
uv run scripts/fetch_hf_dataset.py cc_news 100000 --mode head # fast and biased, smoke tests
uv run scripts/fetch_hf_dataset.py cc_news full -o datasets/cc-news/dataset.csv
```

Mirror first if you plan to cut more than one slice. Sampling against HuggingFace has to
stream all 1.1 GB and took 482 s here; the mirror downloads the same bytes in about 130 s
and every later sample runs off disk in 1.4 s. Results do not change: the 20k sample cut
from the mirror matched the one cut over the network on all 20,000 URLs.

Sampling is the default because the parquet is stored in crawl order. The first 20k rows
cover 273 of the 8,759 domains, while a 20k sample covers 3,641. Corpus term statistics
drive IDF and BM25, so that skew would land straight in the measurements.

Two runs with the same `--seed` drew the identical sample when tested back to back.
Slices are then cut in `hash(url)` order, which makes a smaller size a prefix of a larger
one by construction. DuckDB promises neither across versions, so treat the written file
plus its `.meta.json` as the archived artifact rather than the seed.

`--format parquet` writes the same columns as the CSV slice. `datapipe preprocess`
rejects it until `internal/ingest/reader/parquet_reader.go` is implemented.

## Preprocess and load

```bash
go run ./cmd/datapipe preprocess \
  --input datasets/cc-news/dataset-100k.csv \
  --mapping datasets/cc-news/mapping.yaml \
  --output datasets/cc-news/canonical-dataset.jsonl

DATASET_PATH=datasets/cc-news/canonical-dataset.jsonl MAPPING_ENABLED=false \
ENV_PATHS=cmd/datapipe/articles.env,cmd/datapipe/pg.env \
go run ./cmd/datapipe load articles
```

Field mapping: `datasets/cc-news/mapping.yaml`.
