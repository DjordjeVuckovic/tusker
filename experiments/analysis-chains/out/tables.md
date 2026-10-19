# Generated tables

Source sha256 `4b61eb9518fb8a678afd57270a368dfd65b541604a30ab1ba876dced302b3f76`, seed 20261008, sample A 10,000 (`c2c575fa3b03`), sample B 50,000 (`931a1ded719b`).

## T1. Stem identity, one word at a time

Vocabulary: 87,372 lowercase ASCII-alphabetic words of sample A. Share weighted by document frequency over (article, field) units.

| left | right | words that differ | df-weighted share | most frequent differing words (word, left, right) |
|---|---|---|---|---|
| `pg.snowball_english` | `es.snowball_english` | 10 | 0.077% | added: add/ad, adding: add/ad, ebbing: ebb/eb, erred: err/er, erring: err/er |
| `pg.snowball_english` | `es.stemmer_porter2` | 10 | 0.077% | added: add/ad, adding: add/ad, ebbing: ebb/eb, erred: err/er, erring: err/er |
| `pg.snowball_english` | `pdb.stemmer_english` | 10 | 0.077% | added: add/ad, adding: add/ad, ebbing: ebb/eb, erred: err/er, erring: err/er |
| `pg.snowball_english` | `pystemmer-2.2.0.3.english` | 10 | 0.077% | added: add/ad, adding: add/ad, ebbing: ebb/eb, erred: err/er, erring: err/er |
| `pg.snowball_english` | `snowballstemmer-2.2.0.english` | 10 | 0.077% | added: add/ad, adding: add/ad, ebbing: ebb/eb, erred: err/er, erring: err/er |
| `pg.snowball_english` | `pystemmer-3.0.0.english` | 84 | 0.126% | university: univers/universiti, evening: even/evening, organization: organ/organiz, emergency: emerg/emergenc, organizations: organ/organiz |
| `pg.snowball_english` | `snowballstemmer-3.0.1.english` | 84 | 0.126% | university: univers/universiti, evening: even/evening, organization: organ/organiz, emergency: emerg/emergenc, organizations: organ/organiz |
| `es.snowball_english` | `es.stemmer_porter2` | 0 | 0.000% |  |
| `es.snowball_english` | `pdb.stemmer_english` | 0 | 0.000% |  |
| `es.snowball_english` | `snowballstemmer-2.2.0.english` | 0 | 0.000% |  |
| `pystemmer-3.0.0.english` | `snowballstemmer-3.0.1.english` | 0 | 0.000% |  |
| `es.porter_stem` | `es.stemmer_english` | 0 | 0.000% |  |
| `es.porter_stem` | `pystemmer-2.2.0.3.porter` | 136 | 1.488% | s: s/, is: is/i, as: as/a, us: us/u, technology: technolog/technologi |
| `es.porter_stem` | `nltk-3.9.1.porter.ORIGINAL_ALGORITHM` | 133 | 1.488% | s: s/, is: is/i, as: as/a, us: us/u, technology: technolog/technologi |
| `es.porter_stem` | `nltk-3.9.1.porter.MARTIN_EXTENSIONS` | 0 | 0.000% |  |
| `es.porter_stem` | `nltk-3.9.1.porter.NLTK_EXTENSIONS` | 1,777 | 3.576% | are: ar/are, they: thei/they, one: on/one, day: dai/day, may: mai/may |
| `es.kstem` | `es.stemmer_light_english` | 0 | 0.000% |  |

hunspell, same dictionary file, PG against ES: 1,818 words with different lemma sets; words with more than one lemma: PG 4,147, ES 2,921.

## T2. Tiger against PG `english`

| field | docs | total length | distinct terms | words checked | mismatches |
|---|---|---|---|---|---|
| title | 10,000 | 77,243 | 16,293 | 500 | 0 |
| description | 7,450 | 131,377 | 20,954 | 500 | 0 |
| content | 10,000 | 2,456,678 | 94,943 | 500 | 0 |

## T3. Tokenizer only

Mean per-document Jaccard against `pg.simple` (exact-match share in brackets); n = 10,000 per field, units where both sides are empty left out.

| chain | title | description | content | content vocabulary Jaccard |
|---|---|---|---|---|
| `es.standard` | 0.9389 (70.8%) | 0.9429 (53.9%) | 0.9353 (8.3%) | 0.7250 |
| `es.classic` | 0.9417 (73.2%) | 0.9430 (57.2%) | 0.9453 (11.3%) | 0.7430 |
| `es.letter` | 0.9392 (64.5%) | 0.9489 (52.4%) | 0.9290 (6.4%) | 0.7156 |
| `es.whitespace` | 0.7989 (35.8%) | 0.7385 (6.6%) | 0.6805 (0.1%) | 0.3660 |
| `es.icu` | 0.9389 (70.8%) | 0.9431 (54.1%) | 0.9354 (8.5%) | 0.7255 |
| `es.standard+apos` | 0.9783 (83.3%) | 0.9803 (73.1%) | 0.9701 (18.8%) | 0.7781 |
| `pdb.unicode_words` | 0.9389 (70.8%) | 0.9429 (53.9%) | 0.9353 (8.3%) | 0.7250 |
| `pdb.simple` | 0.9683 (80.8%) | 0.9709 (68.9%) | 0.9586 (15.3%) | 0.7482 |
| `pdb.whitespace` | 0.7989 (35.8%) | 0.7385 (6.6%) | 0.6805 (0.1%) | 0.3660 |
| `pdb.icu` | 0.9389 (70.8%) | 0.9435 (54.3%) | 0.9358 (8.5%) | 0.7275 |

## T4. Full chain

Mean per-document Jaccard against `pg.english` (exact-match share in brackets); n = 10,000 per field, units where both sides are empty left out.

| chain | title | description | content | content vocabulary Jaccard |
|---|---|---|---|---|
| `es.english` | 0.8394 (37.2%) | 0.7751 (10.1%) | 0.7557 (0.1%) | 0.6746 |
| `es.standard+pgstop+snowball` | 0.9558 (76.0%) | 0.9521 (60.9%) | 0.9298 (10.4%) | 0.6867 |
| `es.standard+apos+pgstop+snowball` | 0.9716 (82.2%) | 0.9695 (70.5%) | 0.9562 (16.9%) | 0.7223 |
| `es.icu+pgstop+snowball` | 0.9558 (76.0%) | 0.9524 (61.2%) | 0.9300 (10.5%) | 0.6873 |
| `es.standard+lucenestop+porter2` | 0.9004 (48.7%) | 0.8517 (16.5%) | 0.8223 (0.6%) | 0.6863 |
| `pdb.unicode_words+stem+stop` | 0.9017 (49.2%) | 0.8558 (17.3%) | 0.8258 (0.6%) | 0.6871 |
| `pdb.simple+stem+stop` | 0.8908 (43.9%) | 0.8472 (14.8%) | 0.8272 (0.4%) | 0.6950 |
| `pdb.icu+stem+stop` | 0.9017 (49.2%) | 0.8563 (17.3%) | 0.8260 (0.6%) | 0.6892 |

## T5. What the disagreement is made of

Share of disagreeing (article, term) units by class, `content`. PG-only = terms PG indexes and the chain does not; chain-only = the reverse.

| chain | side | total units | hyphen | contains_digit | slash_or_url | stopword | apostrophe | dotted_abbreviation | dotted_host | email | alpha | non_latin_script | latin_diacritic | symbol_or_emoji |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `es.standard` | PG-only | 80,492 | 34.5% | 15.7% | 7.1% | 17.5% | 0 | 0.0% | 0.2% | 0.7% | 23.8% | 0.3% | 0.1% | 0.0% |
| `es.standard` | chain-only | 59,829 | 0 | 17.3% | 1.3% | 0.2% | 69.8% | 0.0% | 1.2% | 0 | 7.1% | 1.1% | 0.1% | 0.8% |
| `es.standard+apos` | PG-only | 47,970 | 57.8% | 26.2% | 12.0% | 0.1% | 0 | 0.0% | 0.2% | 1.1% | 1.6% | 0.5% | 0.2% | 0.0% |
| `es.standard+apos` | chain-only | 18,076 | 0 | 57.3% | 4.3% | 0.7% | 0 | 0.1% | 4.1% | 0 | 23.5% | 3.7% | 0.3% | 2.7% |
| `pdb.unicode_words` | PG-only | 80,493 | 34.5% | 15.7% | 7.1% | 17.5% | 0 | 0.0% | 0.2% | 0.7% | 23.8% | 0.3% | 0.1% | 0.0% |
| `pdb.unicode_words` | chain-only | 59,854 | 0 | 17.3% | 1.3% | 0.2% | 69.8% | 0.0% | 1.2% | 0 | 7.1% | 1.1% | 0.1% | 0.9% |
| `pdb.simple` | PG-only | 54,394 | 51.0% | 24.5% | 11.9% | 0.1% | 0 | 5.1% | 5.6% | 1.0% | 0.4% | 0.0% | 0.3% | 0.0% |
| `pdb.simple` | chain-only | 30,534 | 0 | 48.6% | 4.3% | 1.7% | 0 | 0 | 0 | 0 | 45.3% | 0.0% | 0.0% | 0.1% |

Share of disagreeing (article, term) units by class, `content`. PG-only = terms PG indexes and the chain does not; chain-only = the reverse.

| chain | side | total units | hyphen | contains_digit | slash_or_url | stopword | apostrophe | dotted_abbreviation | dotted_host | email | alpha | non_latin_script | latin_diacritic | symbol_or_emoji |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `es.english` | PG-only | 133,438 | 20.6% | 10.1% | 4.3% | 0.1% | 0 | 0.8% | 0.7% | 0.4% | 62.5% | 0.2% | 0.1% | 0.0% |
| `es.english` | chain-only | 297,341 | 0 | 3.8% | 0.2% | 62.2% | 4.7% | 0.4% | 0.5% | 0 | 27.6% | 0.2% | 0.0% | 0.2% |
| `es.standard+apos+pgstop+snowball` | PG-only | 51,024 | 53.9% | 25.0% | 11.2% | 0.0% | 0 | 2.2% | 1.7% | 1.0% | 4.1% | 0.5% | 0.2% | 0.0% |
| `es.standard+apos+pgstop+snowball` | chain-only | 21,181 | 0 | 49.6% | 3.6% | 0.0% | 0 | 5.3% | 7.0% | 0 | 25.8% | 3.1% | 0.3% | 2.3% |
| `pdb.unicode_words+stem+stop` | PG-only | 65,940 | 41.7% | 19.4% | 8.7% | 0.5% | 0 | 1.7% | 1.3% | 0.8% | 25.2% | 0.4% | 0.2% | 0.0% |
| `pdb.unicode_words+stem+stop` | chain-only | 234,340 | 0 | 4.5% | 0.3% | 78.8% | 12.4% | 0.5% | 0.6% | 0 | 2.3% | 0.3% | 0.0% | 0.0% |

## T6. Stemmer character

Words with document frequency >= 3 that spaCy also saw: 35,870. spaCy gave 5,609 of them more than one lemma in context; the most frequent is used. Lower UI = fewer missed merges, lower OI = fewer wrong merges. OI values are small because the denominator counts every pair of words from different concepts.

| stemmer | classes | vocabulary reduction | UI vs spaCy | OI vs spaCy | UI vs WordNet | OI vs WordNet |
|---|---|---|---|---|---|---|
| none | 35,870 | 0.0% | 1.000 | 0.00e+00 | 1.000 | 0.00e+00 |
| S-stemmer (es minimal_english) | 30,900 | 13.9% | 0.672 | 1.23e-06 | 0.630 | 9.72e-07 |
| Krovetz (es kstem) | 26,158 | 27.1% | 0.189 | 7.28e-06 | 0.147 | 7.79e-06 |
| Porter (es porter_stem) | 23,704 | 33.9% | 0.082 | 1.86e-05 | 0.069 | 1.98e-05 |
| Porter2, Snowball 2.2 (es snowball) | 23,574 | 34.3% | 0.080 | 1.93e-05 | 0.070 | 2.06e-05 |
| Porter2, PG 18.1 snapshot | 23,574 | 34.3% | 0.079 | 1.93e-05 | 0.069 | 2.06e-05 |
| Porter2, Snowball 3.0 (pystemmer 3.0.0) | 23,565 | 34.3% | 0.079 | 1.92e-05 | 0.069 | 2.05e-05 |
| Lovins (es lovins) | 20,667 | 42.4% | 0.145 | 4.21e-05 | 0.138 | 4.33e-05 |
| hunspell en_US (pg) | 26,831 | 25.2% | 0.288 | 9.03e-06 | 0.265 | 9.74e-06 |
| lemma: spaCy | 27,398 | 23.6% | 0.000 | 0.00e+00 | 0.113 | 3.65e-06 |
| lemma: WordNet morphy | 27,561 | 23.2% | 0.184 | 2.06e-06 | 0.000 | 0.00e+00 |

## T7. Retrieval divergence (proxy queries, sample B)

300 queries. Zero-hit queries per chain: `es.standard` 91, `es.english` 67, `es.standard+pgstop+snowball` 67, `es.standard+apos+pgstop+snowball` 67, `es.standard+lucenestop+porter2` 67, `es.standard+lucenestop+porter` 69, `es.standard+lucenestop+kstem` 75, `es.standard+lucenestop+minimal` 80, `es.standard+lucenestop+hunspell` 70, `pg.english` 67, `pg.english_nostop` 67, `pg.simple` 89, `pg.hunspell` 68.

| engine | left | right | top-10 Jaccard (n) | identical top 10 | RBO ext, p=0.9 (n) |
|---|---|---|---|---|---|
| es | `es.standard` | `es.english` | 0.615 (209) | 27.3% | 0.654 (109) |
| es | `es.standard` | `es.standard+pgstop+snowball` | 0.627 (209) | 29.7% | 0.668 (109) |
| es | `es.standard` | `es.standard+apos+pgstop+snowball` | 0.615 (209) | 27.8% | 0.657 (109) |
| es | `es.standard` | `es.standard+lucenestop+porter2` | 0.636 (209) | 30.6% | 0.681 (109) |
| es | `es.standard` | `es.standard+lucenestop+porter` | 0.642 (209) | 33.5% | 0.685 (109) |
| es | `es.standard` | `es.standard+lucenestop+kstem` | 0.712 (209) | 37.8% | 0.762 (109) |
| es | `es.standard` | `es.standard+lucenestop+minimal` | 0.777 (209) | 47.4% | 0.773 (109) |
| es | `es.standard` | `es.standard+lucenestop+hunspell` | 0.635 (209) | 31.6% | 0.696 (109) |
| es | `es.english` | `es.standard+pgstop+snowball` | 0.884 (233) | 57.1% | 0.877 (136) |
| es | `es.english` | `es.standard+apos+pgstop+snowball` | 0.909 (233) | 65.7% | 0.899 (136) |
| es | `es.english` | `es.standard+lucenestop+porter2` | 0.930 (233) | 75.5% | 0.921 (136) |
| es | `es.english` | `es.standard+lucenestop+porter` | 0.941 (231) | 75.3% | 0.939 (134) |
| es | `es.english` | `es.standard+lucenestop+kstem` | 0.760 (224) | 43.3% | 0.783 (125) |
| es | `es.english` | `es.standard+lucenestop+minimal` | 0.741 (220) | 42.3% | 0.782 (122) |
| es | `es.english` | `es.standard+lucenestop+hunspell` | 0.832 (227) | 58.1% | 0.854 (133) |
| es | `es.standard+pgstop+snowball` | `es.standard+apos+pgstop+snowball` | 0.954 (233) | 79.0% | 0.953 (139) |
| es | `es.standard+pgstop+snowball` | `es.standard+lucenestop+porter2` | 0.939 (233) | 73.0% | 0.931 (138) |
| es | `es.standard+pgstop+snowball` | `es.standard+lucenestop+porter` | 0.882 (231) | 56.3% | 0.869 (134) |
| es | `es.standard+pgstop+snowball` | `es.standard+lucenestop+kstem` | 0.764 (224) | 42.0% | 0.781 (126) |
| es | `es.standard+pgstop+snowball` | `es.standard+lucenestop+minimal` | 0.722 (220) | 36.4% | 0.749 (122) |
| es | `es.standard+pgstop+snowball` | `es.standard+lucenestop+hunspell` | 0.795 (227) | 45.4% | 0.807 (135) |
| es | `es.standard+apos+pgstop+snowball` | `es.standard+lucenestop+porter2` | 0.912 (233) | 63.5% | 0.906 (138) |
| es | `es.standard+apos+pgstop+snowball` | `es.standard+lucenestop+porter` | 0.869 (231) | 53.7% | 0.856 (134) |
| es | `es.standard+apos+pgstop+snowball` | `es.standard+lucenestop+kstem` | 0.753 (224) | 39.3% | 0.769 (126) |
| es | `es.standard+apos+pgstop+snowball` | `es.standard+lucenestop+minimal` | 0.711 (220) | 34.1% | 0.737 (122) |
| es | `es.standard+apos+pgstop+snowball` | `es.standard+lucenestop+hunspell` | 0.796 (227) | 45.4% | 0.812 (135) |
| es | `es.standard+lucenestop+porter2` | `es.standard+lucenestop+porter` | 0.931 (231) | 76.2% | 0.922 (134) |
| es | `es.standard+lucenestop+porter2` | `es.standard+lucenestop+kstem` | 0.790 (224) | 50.4% | 0.819 (126) |
| es | `es.standard+lucenestop+porter2` | `es.standard+lucenestop+minimal` | 0.745 (220) | 44.5% | 0.779 (122) |
| es | `es.standard+lucenestop+porter2` | `es.standard+lucenestop+hunspell` | 0.823 (227) | 55.5% | 0.841 (134) |
| es | `es.standard+lucenestop+porter` | `es.standard+lucenestop+kstem` | 0.798 (224) | 57.1% | 0.828 (125) |
| es | `es.standard+lucenestop+porter` | `es.standard+lucenestop+minimal` | 0.785 (220) | 56.4% | 0.830 (122) |
| es | `es.standard+lucenestop+porter` | `es.standard+lucenestop+hunspell` | 0.828 (227) | 55.9% | 0.846 (132) |
| es | `es.standard+lucenestop+kstem` | `es.standard+lucenestop+minimal` | 0.800 (219) | 58.0% | 0.828 (120) |
| es | `es.standard+lucenestop+kstem` | `es.standard+lucenestop+hunspell` | 0.785 (221) | 51.6% | 0.808 (127) |
| es | `es.standard+lucenestop+minimal` | `es.standard+lucenestop+hunspell` | 0.765 (218) | 47.2% | 0.812 (122) |
| pg | `pg.english` | `pg.english_nostop` | 0.990 (233) | 98.3% | 0.991 (139) |
| pg | `pg.english` | `pg.simple` | 0.684 (211) | 43.6% | 0.759 (114) |
| pg | `pg.english` | `pg.hunspell` | 0.848 (229) | 66.4% | 0.877 (136) |
| pg | `pg.english_nostop` | `pg.simple` | 0.689 (211) | 44.5% | 0.767 (114) |
| pg | `pg.english_nostop` | `pg.hunspell` | 0.846 (229) | 66.4% | 0.871 (136) |
| pg | `pg.simple` | `pg.hunspell` | 0.675 (211) | 45.0% | 0.761 (114) |

## T8. Index size, sample B

ES: bytes of the chain's three sub-fields after force-merge to one segment (`_disk_usage`).

| chain | ES bytes |
|---|---|
| `es.standard` | 47,068,173 |
| `es.english` | 36,630,674 |
| `es.standard+pgstop+snowball` | 33,159,590 |
| `es.standard+apos+pgstop+snowball` | 32,607,981 |
| `es.standard+lucenestop+porter2` | 37,092,130 |
| `es.standard+lucenestop+porter` | 37,319,364 |
| `es.standard+lucenestop+kstem` | 37,992,585 |
| `es.standard+lucenestop+minimal` | 38,693,386 |
| `es.standard+lucenestop+hunspell` | 40,013,982 |

PG: all columns written in one pass, indexes built afterwards, then VACUUM ANALYZE.

| config | tsvector bytes | GIN bytes |
|---|---|---|
| `pg.english` | 118,306,721 | 47,095,808 |
| `pg.english_nostop` | 154,751,653 | 51,191,808 |
| `pg.simple` | 169,113,927 | 58,040,320 |
| `pg.hunspell` | 136,930,302 | 52,420,608 |

## T9. Tokens per article

Mean indexed tokens per article (distinct positions), sample A.

| chain | title | description | content |
|---|---|---|---|
| `pg.english` | 7.7 | 13.1 | 245.7 |
| `pg.simple` | 10.0 | 20.2 | 411.2 |
| `es.english` | 8.1 | 14.6 | 285.4 |
| `es.standard+pgstop+snowball` | 7.6 | 13.0 | 244.0 |
| `es.standard+apos+pgstop+snowball` | 7.6 | 12.9 | 242.6 |
| `es.icu+pgstop+snowball` | 7.6 | 13.0 | 244.0 |
| `es.standard+lucenestop+porter2` | 8.1 | 14.6 | 286.3 |
| `pdb.unicode_words+stem+stop` | 8.1 | 14.5 | 284.7 |
| `pdb.simple+stem+stop` | 8.3 | 15.0 | 292.8 |
| `pdb.icu+stem+stop` | 8.1 | 14.5 | 284.7 |

## T10. ES and ParadeDB compared directly

| pair | title | description | content |
|---|---|---|---|
| `es.standard vs pdb.unicode_words` | 0.999906 | 0.999996 | 0.999974 |
| `es.icu vs pdb.icu` | 0.999982 | 0.999579 | 0.999542 |
| `es.standard vs es.icu` | 0.999789 | 0.999644 | 0.999488 |
| `es.standard+lucenestop+porter2 vs pdb.unicode_words+stem+stop` | 0.998163 | 0.995038 | 0.995252 |

Fed PG's 127 stopwords, Lucene `_english_` drops 33 and ParadeDB `stopwords_language=english` drops 33; the two dropped sets are identical: True.

## T11. ParadeDB cast output against its bm25 index

10,500 terms over 21 (chain, field) aliases; mismatching match counts: 1.
- `pdb.unicode_words/content` `👏🏻`: index 0, cast 1

