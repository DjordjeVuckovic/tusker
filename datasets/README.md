# Datasets

Every dataset has its own folder, and every stage reads and writes a [Frictionless Data Package v2](https://datapackage.org/standard/data-package/): a `datapackage.json` descriptor next to the files it lists.

```
datasets/<ds>/
  README.md                          the source, its quirks, how to fetch it
  mapping.yaml                       raw columns → article fields, read by datapipe preprocess
  raw/                               the source files as published, plus datapackage.json
  canonical/<version>/               datapipe preprocess output
  embeddings/<tag-dir>/<version>/    vectors for one canonical package
  loads/<engine>/<startedAt>.json    one record per datapipe load
```

`<tag-dir>` is the model tag with `:` replaced by `-`, e.g. `qwen3-embedding-0.6b@1024`.

Descriptors and load records are committed. Everything else is gitignored and has to be fetched or regenerated.

## Packages

Each resource is one file, with a `path` relative to the descriptor, its `format`, its size in `bytes` and a `hash` of the form `sha256:<hex>`. Readers check `bytes` and `hash` on every read, so a truncated or altered file is an error and never a short read. `internal/datapackage` is the Go reader and writer.

A descriptor can be opened from a local path, an `https://` URL, or `s3://bucket/key`; a remote location that doesn't end in `.json` is a folder holding `datapackage.json`. For S3-compatible stores such as R2, set `DATASET_S3_ENDPOINT`, `DATASET_S3_REGION` (default `auto` with an endpoint), `DATASET_S3_ACCESS_KEY` and `DATASET_S3_SECRET_KEY`. Set both keys or neither: without them, requests go unsigned. `AWS_*` variables and `~/.aws` are never read. Article text and vectors stay in a private bucket because of publisher copyright; only descriptors are public.

Versions are derived from their inputs, so the same inputs always land in the same folder:

| package    | version                                                                          |
|------------|----------------------------------------------------------------------------------|
| canonical  | `article-v2-` over `articleSchema`, the mapping hash, then the raw resource hashes |
| embeddings | `<recipe>-<dim>-` over the canonical resource hash, model revision, recipe, dim, `maxTokens` |

The suffix is the first 8 hex chars of sha256 over the inputs in that order. Each input is an 8-byte big-endian byte length followed by its UTF-8 bytes. Hashes go in as written in the descriptor (`sha256:<hex>`), integers as plain base-10. `datapackage.Version` implements it in Go.

## Fetching raw data

```bash
uv run scripts/fetch_hf_dataset.py --list
uv run scripts/fetch_hf_dataset.py cc_news --mirror          # Hugging Face shards into raw/
uv run scripts/fetch_kaggle_dataset.py --list
uv run scripts/fetch_kaggle_dataset.py global_news --version <n> # latest when omitted
```

Both write only into `datasets/<ds>/raw/` (`--mirror` is the Hugging Face raw stage) and record the source in `raw/datapackage.json`: the Hugging Face repo and commit sha, or the Kaggle handle and version. A file already in `raw/` with the same hash is kept, and one that differs is refused. `--force` clears `raw/` before fetching and leaves the rest of the dataset folder alone. `--datasets-root` points both at another folder.

`fetch_hf_dataset.py` without `--mirror` still cuts CSV slices into the dataset folder. Those slices are not packages.

Neither fetcher knows about mapping files. They land raw data under its published column names, and `datapipe preprocess` maps those onto articles through `mapping.yaml`.

Script tests run offline against a fake Hub and a prepopulated kagglehub cache:

```bash
uv run --with pytest --with duckdb --with kagglehub pytest -p no:cacheprovider scripts/tests
```

Files from before this layout, such as `dataset-canonical.jsonl` and `embeddings.parquet` at the top of a dataset folder, stay until the packages that replace them verify by hash.
