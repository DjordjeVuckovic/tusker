This dir contains all datasets that have been used by Tusker.

Every dataset lives in its own folder and for convenience should consist of:
- A `README.md` file describing the dataset, its source, and how to use it.
- `dataset.csv` or equivalent raw data file(s).
- Run the preprocessing scripts to generate canonical JSONL files and embeddings. Save these in root dir using the following names for consistency:
  - `canonical-data.jsonl`
  - `embeddings.parquet`
- Create a `mappings.yaml` file to map the raw data fields to the canonical schema.

**Note**: Names and paths are configurable so this is just a suggested convention.

Two fetchers write into these folders, each with a `--list` of the datasets it knows and
a `.meta.json` provenance sidecar next to what it downloads:

```bash
uv run scripts/fetch_hf_dataset.py --list       # HuggingFace, slices or raw shards
uv run scripts/fetch_kaggle_dataset.py --list   # Kaggle, whatever files the owner uploaded
```

Neither fetcher knows about mapping files. They land raw data under its published column
names; mapping those onto the canonical schema is `datapipe preprocess` and its
`mapping.yaml`.