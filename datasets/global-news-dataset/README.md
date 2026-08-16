# Global News Dataset

Kaggle [Global News Dataset](https://www.kaggle.com/datasets/everydaycodings/global-news-dataset).

```
datasets/global-news-dataset/
  dataset.csv                     raw Kaggle export
  mapping.yaml                    raw fields to canonical schema
  canonical-dataset.jsonl         datapipe preprocess output
  canonical-data-report-1.json    preprocess report
  embeddings.parquet              Colab embeddings (qwen3-embedding:0.6b)
```

Payloads are gitignored, so fetch or regenerate them:

```bash
# raw export (needs Kaggle credentials, see scripts/fetch_kaggle_dataset.py --help)
uv run scripts/fetch_kaggle_dataset.py global_news

# ... or straight from the API
curl -L -o ~/Downloads/global-news-dataset.zip \
  https://www.kaggle.com/api/v1/datasets/download/everydaycodings/global-news-dataset
```

Then map it to the canonical form and load it:

```bash
make run-datapipe-preprocess          # dataset.csv to canonical-dataset.jsonl
make run-datapipe-articles-pg         # canonical-dataset.jsonl to postgres
make run-datapipe-embeddings-pg       # embeddings.parquet to article_embeddings
```

Field mapping: `datasets/global-news-dataset/mapping.yaml`.
Benchmarks over this corpus: `tracks/global-news-dataset/`.
