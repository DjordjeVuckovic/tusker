# Embeddings

Document embeddings power the semantic and hybrid search tracks. There are two
ways to produce them, selected by `EMBEDDING_SOURCE`:

| `EMBEDDING_SOURCE` | How                                              | Where                                                        |
|--------------------|--------------------------------------------------|--------------------------------------------------------------|
| `online` (default) | Generated inline during ingestion via Ollama     | `datapipe load articles` (gated by `EMBEDDING_ENABLED=true`) |
| `file`             | Precomputed offline, loaded from an object store | `datapipe load embeddings`                                   |
| `none`             | No embeddings                                    | —                                                            |

The `file` path exists because embedding generation is a one-time, GPU-bound job.

## Offline workflow (`file`)

```
datasets/<ds>/canonical/<version>/      canonical package (preprocess)
  → slim_corpus.py                      id, title, description only; the cloud upload
  → embed_corpus.py on a cloud GPU      Kaggle or Colab; Qwen3-Embedding-0.6B, lead-v1
  → datasets/<ds>/embeddings/<tag-dir>/<version>/
  → datapipe load embeddings            → article_embeddings
```

**Order matters.** Articles have to be ingested first.
`article_embeddings.article_id` is a foreign key to `articles.id`, and
embeddings whose `id` has no matching article are skipped and logged rather than
raised, so a finished multi-GB run can silently load nothing. Confirm the corpus
is in before spending GPU time on it:

```bash
docker compose exec -T pg-news-native psql -U news_user -d news_db -c "select count(*) from articles;"
```

GPU work runs on Kaggle or Colab, never on the laptop. `--device auto` resolves to CUDA
or CPU and never to Apple's MPS.

### Recipes

| Recipe              | Model input                                                                                     | Used for  |
|---------------------|-------------------------------------------------------------------------------------------------|-----------|
| `lead-v1`           | `" ".join` of the stripped, non-empty `title` and `description`. Never the body.                | documents |
| `query-instruct-v1` | `"Instruct: Given a web search, retrieve all relevant news documents\nQuery:" + strip(query)`   | queries   |

There is no space after `Query:`. Both recipes have to match the Go side byte for
byte, `mapDocToPrompt` and `wrapWithInstruct` in `internal/embedding/embedder.go`,
because the API embeds queries through Ollama and those vectors have to land in
the same space. Python's `strip` also removes `\x1c`–`\x1f` and Go's
`strings.TrimSpace` doesn't, so `embed_queries.py` refuses a query that starts or
ends with one.

Both recipes use `Qwen/Qwen3-Embedding-0.6B` pinned to revision
`97b0c614be4d77ee51c0cef4e5f07c00f9eb65b3`, last-token pooling, then a slice to
`--dim` and L2 normalisation. `--dim` defaults to the model's native 1024.
`--max-tokens` (default 512) caps the input tokens and has nothing to do with
the width. The tag carries the width: `qwen3-embedding:0.6b@1024`.

A row with neither a title nor a description is refused before the model loads.
Loaders pair vectors with articles by position, so no row can be skipped. In
cc-news 180,295 of 707,910 canonical rows (25.5%) have no description and embed
as title only. Ids have to be canonical lowercase uuid text, because loaders
compare them byte for byte; anything else is refused rather than rewritten.

A shard or a query whose vector comes out NaN or inf, which fp16 overflow does
silently, is refused before it is written, with the shard number and the first
ids.

### The embeddings package

```
datasets/<ds>/embeddings/qwen3-embedding-0.6b@1024/lead-v1-1024-<8 hex>/
  datapackage.json
  embeddings.parquet        id (string uuid), embedding (list<float32>), canonical row order
```

The descriptor is a Frictionless Data Package v2:

```json
{
  "$schema": "https://datapackage.org/profiles/2.0/datapackage.json",
  "name": "cc-news-embeddings",
  "version": "lead-v1-1024-<8 hex>",
  "language": "eng",
  "embedding": {
    "model": "qwen3-embedding:0.6b",
    "hfModel": "Qwen/Qwen3-Embedding-0.6B",
    "revision": "97b0c614be4d77ee51c0cef4e5f07c00f9eb65b3",
    "tag": "qwen3-embedding:0.6b@1024",
    "dim": 1024,
    "recipe": "lead-v1",
    "pooling": "last_token",
    "normalized": true,
    "maxTokens": 512
  },
  "canonical": {"name": "cc-news", "version": "article-v2-<8 hex>", "resourceHash": "sha256:<hex>"},
  "resources": [
    {"name": "embeddings", "path": "embeddings.parquet", "format": "parquet",
     "mediatype": "application/vnd.apache.parquet", "bytes": 0, "hash": "sha256:<hex>"}
  ]
}
```

The version is `<recipe>-<dim>-` plus the first 8 hex of a sha256 over the
canonical resource hash, model revision, recipe, dim and maxTokens, each input
prefixed with its 8-byte big-endian length. The same inputs always give the same
version, and the same version, batch size and device give byte-identical files.
The input package's resource is checked against its `bytes` and `hash` before
anything is embedded.

### Shrink the upload first

`embed_corpus.py` reads only `id`, `title` and `description`. The article body is
most of the bytes, so project it away before moving anything to a cloud box:

```bash
uv run scripts/slim_corpus.py datasets/cc-news/canonical/<version>/datapackage.json --out /tmp/tusker-payload
```

The output is a package of its own: `articles.parquet` plus a
`slim.datapackage.json` whose `canonical` property names the package it came
from. The descriptor isn't called `datapackage.json` because the kaggle CLI
never uploads a file by that name. `embed_corpus.py`
takes the canonical reference from there, so embedding the slim package gives
the same version and the same bytes as embedding the canonical one. The output
stays plain parquet, since Kaggle decompresses `.gz` uploads and that would break
the recorded hash.

### Kaggle script kernel

`kaggle kernels push` uploads exactly one `code_file`, which is `embed_corpus.py`
itself. The kernel config lives at `datasets/<corpus>/kernel-metadata.json`. The
kernel slug and its `dataset_sources` are per corpus, and `code_file` points back
at the shared script.

Kernels run with no argv. Without a package argument the script reads the one
`slim.datapackage.json` under `/kaggle/input`, writes to `/kaggle/working`, removes
checkpoints after the package is written, and sweeps the batch size.

**0. Verify your phone** at kaggle.com/settings. The CLI sends `enable_gpu` and
`enable_internet`, but the server drops both on unverified accounts, silently. You
get a CPU box with no network and the run dies fetching the model. This is the
first thing to check when a kernel logs `device : cpu`.

**1. Install and authenticate.**

```bash
pip install kaggle          # token from kaggle.com/settings/api -> ~/.kaggle/kaggle.json
```

**2. Upload the slim package as a dataset.**

```bash
uv run scripts/slim_corpus.py datasets/cc-news/canonical/<version>/datapackage.json --out /tmp/tusker-payload
kaggle datasets init -p /tmp/tusker-payload
# edit /tmp/tusker-payload/dataset-metadata.json: set the title, and the id that
# datasets/cc-news/kernel-metadata.json lists under dataset_sources
kaggle datasets version -p /tmp/tusker-payload -m "slim <canonical version>"
```

`version` adds a new version to `djordjevuckovic/tusker-cc-news`, which the
kernel already reads. `kaggle datasets create` fails on a dataset that exists;
use it only for a new dataset, then point `dataset_sources` at that id.

**3. Push the kernel.** Set your username in
`datasets/cc-news/kernel-metadata.json` first. Pushing also starts the run.

```bash
kaggle kernels push -p datasets/cc-news
```

**4. Watch it.**

```bash
kaggle kernels status djordjevuckovic/tusker-embed
kaggle kernels logs   djordjevuckovic/tusker-embed
```

**5. Collect the package.** Only `/kaggle/working` is downloadable. Fetch it to a
scratch dir, then move the version directory in; nothing in `datasets/` is
overwritten.

```bash
kaggle kernels output djordjevuckovic/tusker-embed -p /tmp/tusker-out
mkdir -p datasets/cc-news/embeddings/qwen3-embedding-0.6b@1024
mv /tmp/tusker-out/embeddings/qwen3-embedding-0.6b@1024/lead-v1-1024-<8 hex> \
   datasets/cc-news/embeddings/qwen3-embedding-0.6b@1024/
```

The kernel relies on the Kaggle image's preinstalled transformers, which has to
be at least 4.51 for Qwen3, since a script kernel has no `pip install` step. The
scripts stay on Python 3.11 syntax for the same image.

### Colab notebook

`scripts/embed_colab.ipynb` is the other GPU path. It drives the same scripts from
Drive, for when Kaggle's GPU quota is spent:

1. Copy `scripts/embed_corpus.py` and `scripts/embed_queries.py` to
   `MyDrive/tusker/`, and the slim package (`slim.datapackage.json` plus
   `articles.parquet`) to `MyDrive/tusker/<corpus>/payload/`. The full canonical
   package works too.
2. Open the notebook in Colab with a GPU runtime, set `DATASET` in its settings
   cell, and run the cells in order.
3. The run cell calls `embed_corpus.py` with `--device cuda`, `--dim`,
   `--max-tokens` and checkpoints in `MyDrive/tusker/<corpus>/checkpoints`. After
   a disconnect, re-running the cell resumes from the finished shards.
4. The package lands at `MyDrive/tusker/<corpus>/embeddings/<tag-dir>/<version>/`.
   Copy that version directory into `datasets/<corpus>/embeddings/<tag-dir>/`.

The notebook can't run locally. `scripts/tests/test_colab_notebook.py` checks that
every script call in it parses against the scripts' CLIs, which have abbreviations
turned off so a stale flag fails instead of matching a longer one.

### Query vectors

A track's queries are embedded with the same runtime into
`tracks/<ds>/<paradigm>/trec/query-vectors/`:

```
datapackage.json          embedding {…, recipe: query-instruct-v1}, suite {id, hash}
query-vectors.parquet     query_id, text, embedding; suite order
```

`text` is the query's `description` exactly as the bench runner passes it to the
vector store, in `internal/bench/runner/runner.go`:

```go
vec, err := r.config.VectorStore.QueryVector(ctx, q.Description)
```

The model input is that text wrapped by `query-instruct-v1`. Queries are embedded
one per forward pass, so no query's vector depends on the others in the suite.
`suite.hash` is the sha256 of `suite.yaml`. A later run against an edited suite
refuses to replace the package until it is removed.

On Kaggle, push `scripts/kaggle/queries/kernel-metadata.json`. Its dataset holds
the track's `suite.yaml` and a copy of `scripts/embed_corpus.py`, which
`embed_queries.py` imports:

```bash
mkdir -p /tmp/tusker-queries
cp tracks/cc-news/<paradigm>/suite.yaml scripts/embed_corpus.py /tmp/tusker-queries/
kaggle datasets init -p /tmp/tusker-queries        # set title and id
kaggle datasets create -p /tmp/tusker-queries      # first time; later runs use `kaggle datasets version`
kaggle kernels push -p scripts/kaggle/queries
kaggle kernels output djordjevuckovic/tusker-embed-queries -p /tmp/tusker-queries-out
mv /tmp/tusker-queries-out/query-vectors tracks/cc-news/<paradigm>/trec/
```

### Checkpoints and re-runs

The corpus is streamed and checkpointed every `--shard-size` rows (default
20,000). Shards are cut in canonical row order, so shard N always covers the same
rows. Length sorting happens inside a shard and is undone before the vectors are
saved, which cuts padding waste (2.2x on an M4 Pro) without changing the order.

- The checkpoint dir records the run's settings: input hash, canonical hash,
  model, revision, recipe, dim, maxTokens, shard size, batch size, dtype and
  `--limit`. A run with any other setting is refused instead of mixing shards.
- `--limit N` embeds the first N rows into `<version>-limit-N`, so a trial can
  never be resumed as, or mistaken for, the full package.
- Re-running a finished package verifies it and exits. A package that no longer
  verifies is refused, never overwritten.
- `--clean-checkpoints` drops the shards once the package is written. The merge
  still runs with every shard on disk, so peak disk stays at shards plus Parquet,
  about 5.8 GB for cc-news; only what is left afterwards halves, to 2.9 GB.

`--batch-size` defaults to 64. Wider batches have measured slower on every device
tried so far: on an M4 Pro 64 beat 128 and 256 fell off a cliff, and on a Colab
CUDA GPU the fall-off was monotonic (64 at 199 docs/s, 256 at 158, 512 at 128).
The length sort is why: more, smaller batches pad less than a few wide ones.
`--auto-batch-size` times a 2,000-row slice at 64, 256 and 512 and uses the
fastest. A resumed run reuses the batch size its checkpoints recorded.

### Testing the scripts

The tests build a random one-layer Qwen3 at width 1024 on the fly, run every
script as a CLI on CPU, and never touch the network:

```bash
uv run --no-project --python 3.11 --with-requirements scripts/tests/requirements.txt pytest scripts/tests
```

### Running

```bash
cp cmd/datapipe/embeddings.env.example cmd/datapipe/embeddings.env   # then edit the file path
# The example's model name has no width and would match no query vector.
sed -i.bak 's/^EMBEDDING_MODEL=.*/EMBEDDING_MODEL=qwen3-embedding:0.6b@1024/' cmd/datapipe/embeddings.env
rm cmd/datapipe/embeddings.env.bak
go run ./cmd/datapipe load embeddings
```

Re-runnable: each batch is COPYed into a temp staging table, then upserted
(`ON CONFLICT (article_id, model_name) DO UPDATE`), so partial runs can be
repeated safely.

The loader reads the model name from Parquet metadata, which `embed_corpus.py`
no longer writes; the descriptor holds it, as `embedding.tag`. That is why
`EMBEDDING_MODEL` has to be set to the full tag above.

When loading from S3 the file is first downloaded to a temp file (via
`os.CreateTemp`, cleaned up on exit). The corpus embeddings file can be several
GB — ensure the system temp dir (`$TMPDIR`) has enough space, or set
`EMBEDDING_FILE_PATH` to point at a pre-downloaded local copy.

### Configuration

| Env | Description |
|-----|-------------|
| `EMBEDDING_SOURCE` | must be `file` for this command |
| `EMBEDDING_FILE_PATH` | local Parquet path (skips S3) |
| `EMBEDDING_S3_ENDPOINT` | S3-compatible endpoint (omit for AWS S3) |
| `EMBEDDING_S3_REGION` / `_BUCKET` / `_KEY` | object location |
| `EMBEDDING_S3_ACCESS_KEY` / `_SECRET_KEY` | credentials (falls back to default AWS chain if unset) |
| `EMBEDDING_S3_USE_PATH_STYLE` | `true` for MinIO, `false` for AWS S3 |
| `EMBEDDING_MODEL` | model name stored as `model_name`; required for packages `embed_corpus.py` writes, as the full tag (`qwen3-embedding:0.6b@1024`) |
| `EMBEDDING_BATCH_SIZE` | rows per bulk upsert (default 5000) |
