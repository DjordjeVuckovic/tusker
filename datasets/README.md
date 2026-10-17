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

Git tracks each dataset's `README.md`, `mapping.yaml` and `kernel-metadata.json`, every `datapackage.json`, and the load records under `loads/`. It ignores the data: everything else under `raw/`, `canonical/` and `embeddings/`, and any `.csv`, `.json`, `.jsonl`, `.parquet`, `.npz` or `.gz` file elsewhere in `datasets/`. Fetch or regenerate those.

## Packages

Each resource is one file, with a `path` relative to the descriptor, its `format`, its size in `bytes` and a `hash` of the form `sha256:<hex>`. Readers check `bytes` and `hash` on every read, so a truncated or altered file is an error and never a short read. `internal/datapackage` is the Go reader and writer. Every descriptor must also validate against `api/datapackage-spec/datapackage-v1.json`, which lists only the contract's properties, so a misspelled key fails instead of reading as zero. `make schema-gen` regenerates it from the Go types, and a test fails while the committed file is stale.

A descriptor can be opened from a local path, an `https://` URL, or `s3://bucket/key`; a remote location that doesn't end in `.json` is a folder holding `datapackage.json`. For S3-compatible stores such as R2, set `DATASET_S3_ENDPOINT`, `DATASET_S3_REGION` (default `auto` with an endpoint), `DATASET_S3_ACCESS_KEY` and `DATASET_S3_SECRET_KEY`. Set both keys or neither: without them, requests go unsigned. `AWS_*` variables and `~/.aws` are never read. Article text and vectors stay in a private bucket because of publisher copyright; only descriptors are public.

A canonical, embeddings or query-vectors package's version is the first 8 hex chars of its resource's sha256, so its folder name follows from the bytes: `datapackage.Publish` renames the staged package to `<version>/` and keeps a folder already there, which holds the same bytes. A raw package's version is its source version, the Hugging Face commit or the Kaggle dataset version.

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

Script tests run offline against a fake Hub and a prepopulated kagglehub cache, and validate each raw descriptor against `api/datapackage-spec/datapackage-v1.json`:

```bash
uv run --with pytest --with jsonschema --with duckdb --with kagglehub pytest -p no:cacheprovider scripts/tests
```

Files from before this layout, such as `dataset-canonical.jsonl` and `embeddings.parquet` at the top of a dataset folder, stay until the packages that replace them verify by hash.
