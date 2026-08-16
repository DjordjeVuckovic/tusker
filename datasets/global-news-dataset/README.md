# Global News Dataset

Kaggle [Global News Dataset](https://www.kaggle.com/datasets/everydaycodings/global-news-dataset).

```
datasets/global-news-dataset/
  dataset.csv                          raw Kaggle export
  canonical/
    gl_news_data_canonical.jsonl       datapipe preprocess output
    gl_news_data_report_1.json         preprocess report
    gl_news_embeddings.parquet         Colab embeddings (qwen3-embedding:0.6b)
```

Payloads are gitignored — fetch or regenerate them:

```bash
# raw export (interactive picker, needs ~/.kaggle/kaggle.json)
scripts/datasets/install_kaggle_dataset.sh

# ... or straight from the API
curl -L -o ~/Downloads/global-news-dataset.zip \
  https://www.kaggle.com/api/v1/datasets/download/everydaycodings/global-news-dataset
```

Then map it to the canonical form and load it:

```bash
make run-datapipe-preprocess          # dataset.csv → canonical/*.jsonl
make run-datapipe-articles-pg         # canonical/*.jsonl → postgres
make run-datapipe-embeddings-pg       # canonical/*.parquet → article_embeddings
```

Field mapping: `configs/mappings/gl_news_data_mapping.yaml`.
Benchmarks over this corpus: `tracks/global-news-dataset/`.
