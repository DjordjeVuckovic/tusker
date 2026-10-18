# verify

Scripts behind `../out/verification.md`, run on 2026-10-17 against throwaway containers named
`vf-pg` (pgvector/pgvector:pg18, PG 18.1), `vf-tiger` (timescale/timescaledb-ha:pg18, PG 18.4),
`vf-pdb` (paradedb/paradedb:0.21.5-pg18) and `vf-es` (elasticsearch 8.12.0 on port 55804), with the
hunspell files from `../deps` installed as in the top-level README. They are written independently
of `../scripts` on purpose, so they check the pipeline rather than repeat it.

- `live_stems.py`, `full_stems.py`: probe words and the full vocabulary through every engine's stemmer
- `ref.py`, `full_ref.py`, `pyporter.py`: reference stemmers; run each with `uv run --isolated --no-project --python 3.12 --with <pkg>==<version>`
- `hun.py`: hunspell lemma sets on PG and ES
- `wn.py`: WordNet morphy references
- `retr.py`: re-indexes sample B and re-runs the proxy queries for four chains, comparing with `../out/divergence_results.json`

They write their outputs to the working directory; nothing under `../out` is modified.
