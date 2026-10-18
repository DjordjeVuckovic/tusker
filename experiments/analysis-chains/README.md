# analysis-chains

The analyzer experiment on cc-news: how PostgreSQL, tiger (pg_textsearch), ParadeDB and
Elasticsearch tokenize and stem the same articles, and how the classic stemmers and
lemmatizers differ. Plan: `.claude/plans/analyzer-experiment.md`. Results: `report.md`, with
every table generated into `out/tables.md`.

## Reproduce

Requires Docker, `uv`, and the cc-news canonical file at `datasets/cc-news/dataset-canonical.jsonl`.

1. Containers (throwaway, removed afterwards):

   ```sh
   docker run -d --rm --name ana-pg    -e POSTGRES_PASSWORD=x -p 55701:5432 pgvector/pgvector:pg18
   docker run -d --rm --name ana-tiger -e POSTGRES_PASSWORD=x -p 55702:5432 timescale/timescaledb-ha:pg18
   docker run -d --rm --name ana-pdb   -e POSTGRES_PASSWORD=x -p 55703:5432 paradedb/paradedb:0.21.5-pg18
   docker run -d --rm --name ana-es -e discovery.type=single-node -e xpack.security.enabled=false \
     -e ES_JAVA_OPTS="-Xms1g -Xmx1g" -p 55704:9200 docker.elastic.co/elasticsearch/elasticsearch:8.12.0
   docker exec ana-es bin/elasticsearch-plugin install --batch analysis-icu
   ```

2. hunspell en_US from LibreOffice dictionaries at the commit in `deps/hunspell.source`, copied to
   `ana-es:/usr/share/elasticsearch/config/hunspell/en_US/` and to
   `ana-pg:/usr/share/postgresql/18/tsearch_data/en_us.{affix,dict}`; then `docker restart ana-es`.
   `deps/pg_english.stop` is `english.stop` from the `ana-pg` image.

3. PG configurations, on `ana-pg`:

   ```sql
   CREATE TEXT SEARCH DICTIONARY english_stem_nostop (TEMPLATE = snowball, Language = english);
   CREATE TEXT SEARCH CONFIGURATION english_nostop (COPY = english);
   ALTER TEXT SEARCH CONFIGURATION english_nostop ALTER MAPPING FOR asciiword, asciihword, hword_asciipart, word, hword, hword_part WITH english_stem_nostop;
   CREATE TEXT SEARCH DICTIONARY english_hunspell (TEMPLATE = ispell, DictFile = en_us, AffFile = en_us, StopWords = english);
   CREATE TEXT SEARCH CONFIGURATION english_hunspell (COPY = english);
   ALTER TEXT SEARCH CONFIGURATION english_hunspell ALTER MAPPING FOR asciiword, asciihword, hword_asciipart, word, hword, hword_part WITH english_hunspell, simple;
   CREATE TEXT SEARCH DICTIONARY english_hunspell_nostop (TEMPLATE = ispell, DictFile = en_us, AffFile = en_us);
   ```

4. From `scripts/`, in order:

   ```sh
   uv run python sample.py ../../../datasets/cc-news/dataset-canonical.jsonl ../out
   uv run python collect_pg.py && uv run python collect_pdb.py && uv run python collect_es.py
   uv run python collect_tiger.py
   uv run python stems.py
   uv run python ref_stem.py pystemmer ../out/ref_pystemmer.json
   uv run python ref_stem.py nltk ../out/ref_nltk.json
   uv run --isolated --no-project --python 3.12 --with snowballstemmer==2.2.0 python ref_stem.py snowballstemmer ../out/ref_snowballstemmer_2.2.0.json
   uv run --isolated --no-project --python 3.12 --with snowballstemmer==3.0.1 python ref_stem.py snowballstemmer ../out/ref_snowballstemmer_3.0.1.json
   uv run --isolated --no-project --python 3.12 --with pystemmer==2.2.0.3 python ref_stem.py pystemmer ../out/ref_pystemmer_2.2.0.3.json
   uv run python spacy_lemmas.py
   uv run python agreement.py
   uv run python stem_eval.py
   uv run python divergence.py
   uv run python compare_es_pdb.py
   uv run python check_pdb_index.py
   uv run python review_sample.py
   uv run python tables.py
   uv run --with pytest python -m pytest test_paice.py
   ```

   `--isolated` matters for snowballstemmer: it delegates to PyStemmer whenever PyStemmer is
   importable, and a layered environment would make it report PyStemmer's output. The original
   PyStemmer 2.2.0.3 run was layered over the project environment (PyStemmer 3.0.0); an isolated
   rerun gave identical stems on every vocabulary word (`out/verification.md`, F1.1).

5. Verification: `out/verification.md` re-checks the stemming, lemmatization and analysis claims
   on fresh containers, against raw outputs and against primary sources.

Determinism check: rerun the three `collect_*` scripts with `ANA_TOKENS_DIR=../out/tokens_rerun`
and compare the decompressed dumps; `out/determinism.txt` records the result.
