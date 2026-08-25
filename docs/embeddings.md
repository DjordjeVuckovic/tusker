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
slim_corpus.py     project away the article body   2.0 GB → 76 MB
  → embed_corpus.py on a GPU                       Qwen3-0.6B, last-token pool, L2-norm
  → embeddings.parquet
  → datapipe load embeddings                       → article_embeddings
```

**Order matters.** Articles have to be ingested first.
`article_embeddings.article_id` is a foreign key to `articles.id`, and
embeddings whose `id` has no matching article are skipped and logged rather than
raised, so a finished multi-GB run can silently load nothing. Confirm the corpus
is in before spending GPU time on it:

```bash
docker compose exec -T pg-news-native psql -U news_user -d news_db -c "select count(*) from articles;"
```

### One engine, three places to run it

`scripts/embed_corpus.py` is the whole implementation. It auto-detects CUDA, MPS
or CPU, so the same script backs every path below:

| Where  | Driver                                       | Notes                                    |
|--------|----------------------------------------------|------------------------------------------|
| Laptop | `uv run scripts/embed_corpus.py <corpus>`    | No GPU rental, but slow and hot          |
| Colab  | `scripts/embed_colab.ipynb`                  | Payload on Drive, checkpoints too        |
| Kaggle | `scripts/embed_corpus.py` as a script kernel | Headless round trip via the `kaggle` CLI |

Colab needs a notebook because there is no API for running a script on a free
Colab GPU; the browser cell is the only way onto that machine. Kaggle accepts a
plain `.py` as a script kernel, so it needs no notebook at all.

`scripts/embed_qwen3.ipynb` is the original self-contained Colab notebook that
produced the gl-news vectors. It is kept for provenance. Do not use it for
cc-news, since it loads the entire corpus into memory before embedding anything.

#### Kaggle script kernel

Kaggle runs plain `.py` files, so this path needs no notebook. `kaggle kernels
push` uploads exactly one `code_file`, which is `embed_corpus.py` itself, and the
dataset payload is just the corpus. The kernel config lives at
`datasets/<corpus>/kernel-metadata.json`, since the kernel slug and its
`dataset_sources` are per corpus, and `code_file` points back at the shared
script.

Kernels run with no argv, so omitting the corpus argument switches the script to
Kaggle conventions: it reads `/kaggle/input`, writes to `/kaggle/working`, removes
checkpoints after the merge, and sweeps batch size. Passing a corpus path disables
all of that.

**0. Verify your phone** at kaggle.com/settings. The CLI sends `enable_gpu` and
`enable_internet`, but the server drops both on unverified accounts, silently. You
get a CPU box with no network and the run dies fetching the model. This is the
first thing to check when a kernel logs `device : cpu`.

**1. Install and authenticate.**

```bash
pip install kaggle          # token from kaggle.com/settings/api -> ~/.kaggle/kaggle.json
```

**2. Upload the corpus as a dataset.**

```bash
mkdir -p /tmp/tusker-payload
cp datasets/cc-news/canonical-dataset-slim.jsonl.gz /tmp/tusker-payload/
kaggle datasets init -p /tmp/tusker-payload
# edit /tmp/tusker-payload/dataset-metadata.json: set title and id
kaggle datasets create -p /tmp/tusker-payload
```

`create` fails with "Please upload at least one file" if only the metadata is in
the folder. Kaggle decompresses `.gz` during processing, which is fine, the script
reads either form.

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

**5. Collect the Parquet.**

```bash
kaggle kernels output djordjevuckovic/tusker-embed -p datasets/cc-news/
```

Only `/kaggle/working` is downloadable. The kernel relies on the Kaggle image's
preinstalled transformers, which has to be at least 4.51 for Qwen3, since a script
kernel has no `pip install` step.

### Shrink the upload first

`embed_corpus.py` reads only `id`, `title` and `description`. In cc-news the
article body is 86% of the bytes and none of the input, so project it away before
moving anything to a cloud box:

```bash
uv run scripts/slim_corpus.py datasets/cc-news/canonical-dataset.jsonl
# 708,241 docs -> canonical-dataset-slim.jsonl.gz
# 2029.9 MB -> 75.8 MB  (26.8x smaller)
```

`embed_corpus.py` reads the `.gz` directly and produces the same vectors as the
unprojected file. Ids are random UUIDs assigned at preprocess time
(`document.NewArticleID`), not hashes, so they cannot be regenerated upstream and
have to travel with the text.

The return trip is the harder one. The Parquet is about 2.9 GB for cc-news
(708,241 × 1024 × 4 bytes), and `--clean-checkpoints` drops the shard files once
the merge succeeds, halving peak disk from roughly 5.8 GB.

### How it behaves

The JSONL is streamed and checkpointed every 20k documents. Re-running skips
finished shards, so a dropped session resumes.

Shards are cut in strict file order, so shard N always addresses the same
documents. Length sorting happens inside a shard and is undone before the vectors
are saved, which keeps resume exact while still cutting padding waste (2.2x on an
M4 Pro).

`--batch-size` defaults to 64, measured on Apple Silicon where wide batches thrash
unified memory (64 beat 128, and 256 fell off a cliff). A dedicated GPU usually
wants more, so `--auto-batch-size` times a 2,000-doc slice at 64, 256 and 512 and
uses the fastest. Both cloud paths turn it on.

### Embedded fields

Default is title plus description, matching how gl-news was embedded. Its
`content` was API-truncated (`"… [+1826 chars]"`), so there was never a real body
to use there.

cc-news does have real bodies (100% filled, median 1635 chars), and 18.8% of its
documents have no description, leaving those as title-only vectors (median 59
chars). If the semantic track looks weak on cc-news, `--full-content` on both
`slim_corpus.py` and `embed_corpus.py` is the lever, at roughly a 700 MB upload
and a substantially slower run. Each corpus is its own benchmark track, so the
PG-vs-ES comparison holds either way.

### Artifact format

Parquet with two columns:

| Column | Type | Description |
|--------|------|-------------|
| `id` | `string` | article UUID |
| `embedding` | `list<float32>` | 1024-dim, L2-normalised |

Plus file-level metadata read by the loader:

| Key | Purpose |
|-----|---------|
| `model` | stored as `article_embeddings.model_name`; must match the query-time model |
| `hf_model_id`, `dim`, `pooling`, `normalized`, `row_count`, `created_at` | provenance / validation |

The loader uses `model` from the file so document-side and query-side embeddings
always agree on `model_name`. Override with `EMBEDDING_MODEL` if needed.

### Running

```bash
cp cmd/datapipe/embeddings.env.example cmd/datapipe/embeddings.env   # then edit
go run ./cmd/datapipe load embeddings
```

Re-runnable: each batch is COPYed into a temp staging table, then upserted
(`ON CONFLICT (article_id, model_name) DO UPDATE`), so partial runs can be
repeated safely.

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
| `EMBEDDING_MODEL` | optional override of the file's `model` metadata |
| `EMBEDDING_BATCH_SIZE` | rows per bulk upsert (default 5000) |
