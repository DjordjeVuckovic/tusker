# Verification ledger

Re-verification of the analyzer experiment's stemming, lemmatization and analysis claims, plus the fuzzy-search design facts, done on 2026-10-17. Each claim gets up to three independent checks:

- **(a) recompute:** the number is recomputed from raw files in `out/` (token dumps, `vocab.json`, `divergence_results.json`, `spacy_lemmas.json`) with new code, not by re-reading `tables.md`.
- **(b) re-execute:** the claim is re-run on fresh throwaway containers with the same image IDs as the original run (`versions.json`), or in isolated `uv --isolated` environments. It uses new scripts, not `scripts/`.
- **(c) source:** a primary source was opened: code at a release tag, official docs, or the paper. Quotes were checked against the downloaded text; checks done only by a source agent are marked as such.

The scripts behind (a) and (b) are in `../verify/`. The fetched papers, docs and engine source files behind (c) are in `../sources/` (gitignored, since most are copyrighted).

Verdicts: **confirmed**, **corrected** (the claim was wrong or imprecise; the report now says what is below) or **open** (not verifiable here).

Containers used for (b): `pgvector/pgvector:pg18` (PostgreSQL 18.1), `timescale/timescaledb-ha:pg18` (PostgreSQL 18.4, pg_textsearch 1.1.0), `paradedb/paradedb:0.21.5-pg18`, `elasticsearch:8.12.0` (Lucene 9.9.1). Image IDs match `versions.json`. hunspell files match the sha256 values recorded there.

## Inputs

| # | Claim | (a) | (b) | (c) | Verdict |
|---|---|---|---|---|---|
| I1 | Vocabulary is 87,372 lowercase ASCII-alphabetic words of sample A, with df over (article, field) units | Recomputed from `tokens/pg.simple.jsonl.gz`: 87,372 words, df identical for every word | n/a | n/a | confirmed |
| I2 | Sample B has no NULL text fields | 0 NULL, 12,689 empty strings | n/a | n/a | confirmed (matters because `setweight(...) \|\| NULL` is NULL in PG) |

## Finding 1: Porter2 differs between engines

| # | Claim | (a) | (b) | (c) | Verdict |
|---|---|---|---|---|---|
| F1.1 | PG 18.1 differs from ES `snowball`, ES `porter2`, ParadeDB and Snowball 2.2.0 on the same 10 words, 0.077% df-weighted | n/a | All 87,372 words re-stemmed live: PG vs each of ES snowball, ES porter2, ParadeDB, isolated snowballstemmer 2.2.0 and isolated PyStemmer 2.2.0.3 = 10 words, 0.077%; the word list is identical | n/a | confirmed |
| F1.2 | ES `snowball`, ES `porter2`, ParadeDB and Snowball 2.2.0 are identical on every word | n/a | 0 differences, live | n/a | confirmed |
| F1.3 | PG 18.1 stemmers are Snowball commit `d19326ac` (2025-02-19); that commit includes the 2023-10-27 undoubling change and no English change from 2025-03-17 on | n/a | n/a | PG `src/backend/snowball/README` at `REL_18_1`; `gh api` commit dates; `compare/99f4cfd…d19326ac` = ahead by 78; the first post-2025-02-19 English commit (029b41ca, 2025-03-17) is a descendant of d19326ac | confirmed. Note: d19326ac itself is a Dutch fix, not an English one |
| F1.4 | PG 18.4 (tiger) ships the same stemmer as 18.1 (was open) | n/a | All 87,372 words: PG 18.1 vs 18.4 = 0 differences | README identical at `REL_18_1` and `REL_18_4`; `stem_UTF_8_english.c` byte-identical (source agent) | confirmed, item closed |
| F1.5 | Snowball 3.0 differs from PG on 84 words and from ES on 94 | n/a | Isolated PyStemmer 3.0.0 and snowballstemmer 3.0.1: PG 84 (0.126%), ES 94 (0.202%). 3.0 agrees with PG on all 10 undoubling words, so 94 = 84 + 10 | Snowball NEWS 3.0.0 lists the English changes (evening, universe/university, emergency, organ, past/paste, lateral, -ogist) | confirmed; the ES-vs-3.0 row was missing from `tables.md` and is recorded here |
| F1.6 | Which Snowball snapshot Lucene and ParadeDB use "is not identified" (was open) | n/a | n/a | Lucene 9.9.1 `gradle/generation/snowball.gradle` pins `snowballStemmerCommit = d8cf01dd` (2020-11-11), 247 commits before the undoubling fix. ParadeDB 0.21.5 uses rust-stemmers 1.2.0, whose `english.sbl` was imported 2017-02 and has no `aeo` grouping. `algorithms/english.sbl` had no content change between 2017 and 2023-10-27 (one 2018 commit is a pure rename) | **corrected**: Lucene is identified (d8cf01dd). ParadeDB is bounded (2017 import, no release named). Both sit in the period where the English algorithm did not change, which is why they equal Snowball 2.2.0 |
| F1.7 | A query for `added` matches documents with `ad` or `ads` on ES, not on PG | n/a | Live match queries: ES (standard + snowball, mapping checked) returns all three documents for `added`, `ads` and `ad`; PG `added` returns only "added", PG `ad`/`ads` return "ad" and "ads" | n/a | confirmed |

## Finding 2: tiger

| # | Claim | (a) | (b) | (c) | Verdict |
|---|---|---|---|---|---|
| F2.1 | tiger analyzes exactly as PG `english` | n/a | Stemmer identical (F1.4) | pg_textsearch 1.1.0 calls `to_tsvector_byid` with the configured regconfig (`build.c`, source agent) | confirmed |
| F2.2 | (earlier conversation) "tiger offers nothing beyond `english`" | n/a | A bm25 index with `text_config='public.vf_hunspell'` (a custom ispell/hunspell configuration) builds. With the bare name `vf_hunspell` it fails with "text search configuration does not exist" | README: "Works with Postgres text search configurations" | **corrected**: tiger accepts any PG text search configuration, including hunspell, but a custom one must be schema-qualified |

## Finding 3: other identities, hunspell, lemmatization

| # | Claim | (a) | (b) | (c) | Verdict |
|---|---|---|---|---|---|
| F3.1 | ES `stemmer: english` = `porter_stem` | n/a | 0 differences on 87,372 words | `StemmerTokenFilterFactory` @v8.12.0: `english` → `PorterStemFilter`; `PorterStemTokenFilterFactory` → `PorterStemFilter` | confirmed |
| F3.2 | ES `porter_stem` = NLTK `MARTIN_EXTENSIONS`, 0 words differ | n/a | 0 differences, isolated nltk 3.9.1 | Lucene `PorterStemmer` javadoc: derived from Porter's "Release 3"; tartarus.org lists three departures from the 1980 paper (`logi`→`log`, `bli`→`ble`, length-1/2 words untouched) | confirmed |
| F3.3 | ES `porter_stem` differs from NLTK `ORIGINAL_ALGORITHM` on 132 words and from libstemmer `porter` on 135, 0.986% | n/a | 133 and 136 words, 1.488% | n/a | **corrected**: the extra word is `s`, which both 1980-style stemmers reduce to an empty string. The original pipeline (`stem_eval.single`) maps an empty stem back to the word itself, which hid it. `s` carries 0.50% of the df weight |
| F3.4 | ES `light_english` = `kstem` | n/a | 0 differences | Factory maps both to `KStemFilter` | confirmed |
| F3.5 | hunspell, same files: PG and ES lemma sets differ on 1,818 words; multi-lemma words PG 4,147, ES 2,921 | n/a | Re-run live: 1,818 / 4,147 / 2,921 | n/a | confirmed |
| F3.6 | hunspell is not a lemmatizer: `better` and `was` stay unchanged | spaCy (`spacy_lemmas.json`): went→go 1,303 of 1,305, mice→mouse 39 of 41, was→be 25,997 of 26,000, children→child 1,490 of 1,620 | Live `ts_lexize` and ES hunspell: better, was, went, mice and children unchanged; best → {best, b}; dying → {dying, dye} | hunspell(5): affix rules, one dictionary item per stem, no context. spaCy docs: rule-based, POS-dependent for English | confirmed and extended |
| F3.7 | (new) hunspell en_US does not recognise 43,618 of the 87,372 vocabulary words (`ts_lexize` NULL); in PG's `english_hunspell` configuration they fall through to `simple` | n/a | Live | PG docs: "Ispell dictionaries usually recognize a limited set of words, so they should be followed by another broader dictionary" | new |

## Finding 4 and 5: tokenization and full chains

| # | Claim | (a) | (b) | (c) | Verdict |
|---|---|---|---|---|---|
| F4.1 | T3 and T4 Jaccard values (e.g. aligned chain 0.9716 / 0.9695 / 0.9562; ParadeDB unicode_words 0.9017 / 0.8558 / 0.8258; ES english 0.8394 / 0.7751 / 0.7557) | Recomputed from token dumps, same to 4 decimals, exact-match shares too | n/a | n/a | confirmed |
| F4.2 | T9 tokens per article (PG english 7.7 / 13.1 / 245.7; ES english 8.1 / 14.6 / 285.4; ParadeDB 8.1 / 14.5 / 284.7) | Recomputed: identical | n/a | n/a | confirmed. ParadeDB content is 15.9% longer than PG's (`notes/parity.md` said ~18%, from an earlier 1,500-document sample) |
| F4.3 | `U.S.`: PG `u.s`, ES `u.` | n/a | ES standard tokenizer → `u.s`, then Porter or Porter2 → `u.`; ParadeDB unicode_words → `u.s` → `u.`; PG parses `U.S` as token type `file` (not stemmed) → `u.s` | UAX #29 WB6/WB7: no break between letters around MidNumLet `.` | confirmed; the `u.` comes from the stemmer, not the tokenizer |
| F4.4 | `don't`: PG splits into `don` + `t`, both stopwords; ES keeps `don't`. `info@reuters.com`: PG one email token, ES `info` + `reuters.com` | n/a | Live | PG parser docs (email token type) | confirmed |
| F5.1 | PG, Lucene `_english_` and ParadeDB stopword lists: Lucene and ParadeDB drop the same 33 of PG's 127 | n/a | Live: PG list sha256 matches `deps`; 33 = 33, identical; all 33 Lucene words are in PG's list | Lucene `EnglishAnalyzer.ENGLISH_STOP_WORDS_SET`; tantivy fork `stop_word_filter` English list (comment: "the same list of words used by ... Lucene") | confirmed. The `tables.md` sentence was ambiguous and now reads: the 33-word lists are identical and are a subset of PG's 127 |
| F5.2 | ParadeDB applies stopwords after stemming; ES and PG before | n/a | ParadeDB drops `beings`, `it's`, `that's`, `theirs` (stems `be`, `it`, `that`, `their` are stopwords); ES standard + `_english_` + porter2 keeps them | ParadeDB `tokenizers/src/manager.rs` @v0.21.5: `.stemmer()` then `.stopwords_language()`; PG `dict_snowball.c`: stop-list check on the unstemmed word, then stem | confirmed |
| F5.3 | `stopwords=` on `pdb.*` casts is accepted and ignored | n/a | (original run) | `typmod/mod.rs` @v0.21.5: `stopwords: None, // TODO`; validation accepts the key | confirmed |
| F5.4 | ES built-in `english` differs from PG "through stopwords (Lucene's 33) and the Porter stemmer" | n/a | n/a | Lucene `EnglishAnalyzer.createComponents`: StandardTokenizer → EnglishPossessiveFilter → LowerCaseFilter → StopFilter → (SetKeywordMarkerFilter only when stem exclusions are set) → PorterStemFilter | **corrected**: the possessive filter is part of the chain too (`it's` → `it`, then dropped as a stopword) |

## Finding 6: Paice indices

| # | Claim | (a) | (b) | (c) | Verdict |
|---|---|---|---|---|---|
| F6.1 | T6 classes, reduction, UI and OI against spaCy and WordNet for all 11 rows | Recomputed with an algebraically different implementation (pair counts: GUMT = Σ C(n_g,2) − Σ C(n_gs,2), GWMT = Σ C(n_s,2) − Σ C(n_gs,2), GDNT = C(W,2) − Σ C(n_g,2)) | Inputs are fresh live stems from F1, live hunspell from F3.5, WordNet morphy re-run in isolated nltk 3.9.1 (WordNet 3.0) | Paice's Lancaster site: UI = 1 − CI over equivalent pairs, OI = 1 − DI over non-equivalent pairs; NLTK `metrics/paice.py`: OI = GWMT / GDNT | confirmed, every cell identical |
| F6.2 | Krovetz wrongly merges about 38% as often as Porter2; Lovins OI is more than twice Porter's | Ratios 0.377 and 2.26 | | | confirmed |
| F6.3 | hunspell "wrongly merges slightly more often than Krovetz" | Ratio 1.24 | | | **corrected**: about 24% more often |
| F6.4 | spaCy gives 5,609 of 35,870 words more than one lemma | Recomputed: 5,609 | | | confirmed |
| F6.5 | Paice 1994 and 1996 full texts | | | Paywalled, not opened; definitions come from Paice's own site plus the NLTK implementation | open (secondary source for the formulas) |

## Finding 7: retrieval divergence

| # | Claim | (a) | (b) | (c) | Verdict |
|---|---|---|---|---|---|
| F7.1 | Stored rankings are reproducible | n/a | Re-indexed sample B with new loader code on fresh containers and re-ran all 300 queries for `es.standard+lucenestop+porter2`, `es.standard+lucenestop+minimal`, `pg.english` and `pg.simple`: 300/300 identical ordered top-10 lists for each | n/a | confirmed |
| F7.2 | Mean top-10 Jaccard values and zero-hit counts | Recomputed from `divergence_results.json`: every Jaccard and every zero-hit count identical | | | confirmed |
| F7.3 | RBO ext, p = 0.9 | Recomputed with an independent implementation of Webber et al. (2010) eq. 32 for three pairs: identical | | | confirmed |
| F7.4 | "identical top 10" share | 57.1% etc. reproduce only as **same set, order ignored**; same order is lower (e.g. ES `english` vs `es.standard+pgstop+snowball` 36.5%, Porter2 vs S-stemmer 37.7%) | | | **corrected** in wording |
| F7.5 | "about 1 to 1.5 different documents per query" for stemmer-family changes | Measured directly as half the mean symmetric difference of the two top-10 sets: hunspell 1.07, Krovetz 1.25, S-stemmer 1.48 | | | confirmed, now measured instead of derived from Jaccard |

## Statements from the earlier conversation (not in the report)

| # | Statement | Check | Verdict |
|---|---|---|---|
| E1 | "PG, Lucene/ES and tantivy all generate their stemmers from the same Snowball source, which is why they produce identical stems" | F1.1, F1.6 | **wrong**: three different Snowball snapshots; PG differs on 10 words |
| E2 | "Snowball is a language Martin Porter created (2001)" | snowballstem.org introduction: "M.F. Porter October 2001" | confirmed |
| E3 | "studies → studi, running → run; universe and university both become univers" | Live: true for Porter2 in PG, ES and Snowball 2.2.0. Snowball 3.0 stems `university` → `universiti`, so they no longer merge there | confirmed, with the 3.0 caveat |
| E4 | "Porter (ES `english`) reduces news → new; Porter2 keeps news" | Live; Porter2 page lists `news` as invariant ("news is not the plural of new") | confirmed |
| E5 | "Lemmatization: studies → study, better → good, was → be" | spaCy on cc-news: studies → study (204 of 245), was → be; `better` → `well` in 1,503 of 1,547 (en_core_web_sm), not `good` | better → good is the textbook example; this model gives `well`. Use `was → be`, `went → go`, `mice → mouse` |
| E6 | "Harman 1991, Hull 1996, Kraaij & Pohlmann 1996 found suffix stripping captures most of the gain; differences between methods are small" | Harman (abstract): no significant improvement for any of three suffixing algorithms. Hull (secondary, via Kraaij & Pohlmann): stemming almost always beneficial except long queries at low recall, no significant difference between Porter, Lovins and linguistic stemmers. Kraaij & Pohlmann (Dutch): inflectional linguistic stemming significantly better than full linguistic and non-linguistic stemming | **wrong as stated**. The literature says the effect for English is small and query-dependent (Manning et al.: "at most very modest benefits"; "helps a lot for some queries, ... equally hurts ... others") |
| E7 | "Lin et al. 2016 found BM25 baselines differed mostly because of tokenization and stemming, not BM25 itself" | Paper: seven systems on Gov2, BM25 runs from ATIRE, Lucene, MG4J and Terrier, others query likelihood, SDM, DPH or impact-ordered; "many of them purport to implement the same model (e.g., BM25)"; "We believe that these differences can be attributed to relatively uninteresting differences in document pre-processing, tokenization, stemming, and stopwords". Most differences were not significant; the systems also used different BM25 parameters | **overstated**: a hedged attribution, not a finding, and it never says "not BM25 itself" |
| E8 | "Snowball is the standard TREC baseline" | Lin et al. 2016: only MG4J used Porter2; Lucene and Terrier Porter, Indri and Galago Krovetz. Anserini 2.3.0 defaults to `porter` | **wrong**: Porter (1980) is the common baseline |
| E9 | "ParadeDB and tiger can't run a dictionary-based normaliser" | F2.2 | **half wrong**: tiger can (any PG configuration); ParadeDB 0.21.5 has no hunspell or dictionary filter |
| E10 | "ES built-in english: original Porter, 33-word stoplist, possessive filter" | F5.4 | confirmed |

## Fuzzy-search design facts

| # | Claim | (b) | (c) | Verdict |
|---|---|---|---|---|
| Z1 | ES `AUTO`: 0 edits for 1–2 characters, 1 for 3–5, 2 for longer, measured on the analyzed term | Live: `ac` with AUTO does not match `ab`, with `fuzziness: 1` it does; `vite` → `vote`, `tix` → `tax` with AUTO | ES docs common-options; `Fuzziness.asDistance` counts code points; `MatchQueryParser.newTermQuery` applies it to the analyzed term | confirmed |
| Z2 | ES counts a transposition as 1 edit by default; fuzzystrmatch has only plain Levenshtein | Live: `from` → `form` with AUTO (4 chars, 1 edit) only when `fuzzy_transpositions: true`. PG `levenshtein('form','from') = 2` | Lucene FuzzyQuery: "Damerau-Levenshtein (optimal string alignment)", `defaultTranspositions = true`; fuzzystrmatch docs list no Damerau function | confirmed |
| Z3 | `max_expansions` 50, `prefix_length` 0; ES keeps the closest terms | n/a | Lucene constants; `FuzzyTermsEnum` boost = 1 − ed / min(len); `TopTermsRewrite` keeps the highest boost, ties to the lexicographically smaller term; df does not decide which terms are kept | confirmed, refined: closeness is edit distance scaled by the shorter term's length |
| Z4 | Fuzzy terms in `match` are rewritten with blended frequencies | n/a | ES match docs: default `fuzzy_rewrite` is `top_terms_blended_freqs_${max_expansions}`. Lucene javadoc: "adjusts the frequencies used for scoring to be blended across the terms, otherwise the rarest term typically ranks highest". BlendedTermQuery: df = max, ttf = sum | confirmed (the word "misspellings" is ours, not Lucene's) |
| Z5 | (new) `multi_match` `cross_fields` does not accept fuzziness | Live: "Fuzziness not allowed for type [cross_fields]" | `MultiMatchQueryBuilder` | new: our API's multi_match (`cross_fields`) takes no fuzziness, so this is not a bug, but fuzzy parity is per field or `best_fields` only |
| Z6 | pg_trgm `%` default threshold 0.3; padding two spaces before, one after | `show_trgm('cat')` = {"  c"," ca","at ","cat"}; threshold 0.3 | pg_trgm docs; `trgm_op.c` uses `>=` (docs say "greater than") | confirmed |
| Z7 | A trigram pre-filter misses real one-typo candidates in short words | Live at 0.3: `vote`/`vite` 0.250, `tax`/`tix` 0.143 (both 1 edit) fail; transpositions `form`/`from` 0.111 and `news`/`nwes` 0.111 fail; longer words pass (`government`/`goverment` 0.615) | n/a | confirmed |
| Z8 | "tens of thousands of strings" within 2 edits of a 6-letter word | Counted: 51,040 strings within 2 edits of `change` (333 within 1), 26-letter alphabet | n/a | confirmed |
| Z9 | `levenshtein_less_equal` returns the exact distance when it is at most max_d, otherwise some value greater than max_d | n/a | fuzzystrmatch docs; 255-character limit | confirmed |
| Z10 | `ts_stat` can count only some weights | n/a | PG docs: "If weights is supplied, only occurrences having one of those weights are counted." | confirmed |
| Z11 | PG `<N>` keeps word order and is an exact distance; ES slop is "up to N, any order, a swap costs 2" | Live: `change climate` does not match the slop-2 build for "climate change" on PG 18.1; ES slop 2 matches it | PG textsearch-intro (`<2>` allows exactly one lexeme between); ES match_phrase docs ("Transposed terms have a slop of 2") | confirmed |
| Z12 | The ES tie-break among equal-distance candidates | n/a | `TopTermsRewrite.ScoreTerm.compareTo`: equal boost → term bytes | closed (was open) |

## Not re-verified

- The Krovetz (1993) quotes (the conclusion, and "the Porter stemmer removes about 60 suffixes"): the PDF's text layer is garbled. The source agent read it from rendered page images; I could not render pages here.
- Harman (1991), Hull (1996) and Paice (1994, 1996): full texts paywalled. Only abstracts or secondary accounts were opened, and they are cited as such.
- The LLM-tokenizer characters-per-token figures from the earlier conversation are outside this verification's scope.
