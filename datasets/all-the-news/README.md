# All the News 2.0

HuggingFace [rjac/all-the-news-2-1-Component-one](https://huggingface.co/datasets/rjac/all-the-news-2-1-Component-one),
a mirror of the [Components One](https://components.one/datasets/all-the-news-2-news-articles-dataset)
*All the News 2.0* corpus. "Component-one" in the repo name is the publisher, not a
subset: the mirror carries the whole thing.

|              |                                                                                                                              |
|--------------|------------------------------------------------------------------------------------------------------------------------------|
| Articles     | 2,688,878                                                                                                                    |
| Publications | 27 US outlets                                                                                                                |
| Period       | 2016-01 to 2020-04                                                                                                           |
| Language     | English                                                                                                                      |
| Columns      | `title`, `article`, `author`, `url`, `date`, `publication`, `section` (+ unused `idx`, `article_idx`, `year`, `month`, `day`) |
| On HF        | 36 parquet shards, ~5.3 GB                                                                                                   |

`section` is null for roughly half the corpus and `author` for about a third, so both
land on optional targets. There is no description column, which is the mirror image of
`cc-news`: descriptions but no author. `publication` is a display name such as `Vox` or
`Business Insider`, where `cc-news` supplies a hostname for the same canonical field.

```
datasets/all-the-news/
  raw/                       mirrored HuggingFace shards (--format raw)
  dataset-100k.csv           slice written by scripts/fetch_hf_dataset.py
  dataset-100k.meta.json     provenance: repo, mode, seed, row count
  canonical-dataset.jsonl    datapipe preprocess output
  embeddings.parquet         Colab embeddings (qwen3-embedding:0.6b)
```

Payloads are gitignored, so fetch or regenerate them.

## Fetch

```bash
uv run scripts/fetch_hf_dataset.py all_the_news --format raw        # mirror the shards once
uv run scripts/fetch_hf_dataset.py all_the_news 100000              # reservoir sample (default)
uv run scripts/fetch_hf_dataset.py all_the_news 100000 500000       # nested slices from one scan
uv run scripts/fetch_hf_dataset.py all_the_news 100000 --mode head  # fast and biased, smoke tests
uv run scripts/fetch_hf_dataset.py all_the_news full -o datasets/all-the-news/dataset.csv
```

This corpus is five times the size of `cc-news`, so mirror before slicing unless you only
ever want one cut. Sampling over the network re-streams all 5.3 GB each time; against a
local mirror it reads from disk.

Sampling is the default because the shards are not evenly mixed by outlet: the first 20k
rows carry 7 of the 27 publications, and a corpus that narrow shifts the term statistics
the IR metrics are computed from.

`--format parquet` writes the same columns as the CSV slice. `datapipe preprocess`
rejects it until `internal/ingest/reader/parquet_reader.go` is implemented.

## Preprocess and load

```bash
go run ./cmd/datapipe preprocess \
  --input datasets/all-the-news/dataset-100k.csv \
  --mapping datasets/all-the-news/mapping.yaml \
  --output datasets/all-the-news/canonical-dataset.jsonl

DATASET_PATH=datasets/all-the-news/canonical-dataset.jsonl MAPPING_ENABLED=false \
ENV_PATHS=cmd/datapipe/articles.env,cmd/datapipe/pg.env \
go run ./cmd/datapipe load articles
```

Field mapping: `datasets/all-the-news/mapping.yaml`.
