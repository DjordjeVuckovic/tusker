# Manual review of 50 disagreements (`review_sample.py`, seed 20261008)

Each line: number, side, automatic class, term, my verdict on the cause. The context window is
the first match of the term's first four characters, so for a few terms (2, 10, 19, 23) it shows
a different occurrence than the one that caused the disagreement; the cause there is inferred
from the term, not seen in the window.

Shape class correct: 50 of 50. Labels revised after review: `url` renamed `slash_or_url`
(items 3, 20 and 22 are slash compounds, not URLs), and `stopword` split out of `alpha` (18 of
the 25 ParadeDB items were stopwords that its 33-word list keeps).

| # | side | class | term | cause |
|---|---|---|---|---|
| 1 | ES | digit | `40` | `25-40`: PG reads `-40` as a signed integer |
| 2 | PG | digit | `2` | number split by PG's parser; occurrence not in window |
| 3 | PG | slash | `registration/licence` | PG keeps slash compounds as one `file` token |
| 4 | PG | hyphen | `stats-wis` | PG keeps hyphenated word whole plus parts |
| 5 | PG | hyphen | `hartley-nagl` | same |
| 6 | PG | digit | `003` | `4,003`: PG splits at the comma, ES keeps `4,003` |
| 7 | PG | hyphen | `anti-gay` | hyphen whole |
| 8 | ES | alpha | `singer` | `actress/singer`: PG keeps the slash compound |
| 9 | PG | hyphen | `osa-oviaw` | hyphen whole |
| 10 | ES | digit | `02` | number tokenization; occurrence not in window |
| 11 | ES | alpha | `sc` | `SC/STs`: slash compound in PG |
| 12 | ES | digit | `172.5m` | ES keeps `172.5m`; PG splits number and unit |
| 13 | PG | digit | `7.30` | `7.30pm`: PG splits number and unit |
| 14 | PG | hyphen | `soldier-husband` | hyphen whole |
| 15 | ES | digit | `8new` | `8News`: ES stems it; PG maps digit-words to `simple`, unstemmed |
| 16 | PG | hyphen | `up-and-com` | hyphen whole |
| 17 | ES | digit | `637` | `.333/.459/.637`: PG keeps the slash run |
| 18 | ES | digit | `2` | `1/2`: PG keeps it as one token |
| 19 | PG | alpha | `k` | single-letter token; occurrence not in window |
| 20 | PG | slash | `news/getty` | photo credit `News/Getty`, slash compound |
| 21 | ES | alpha | `optic` | `Optics/Kier`, slash compound in PG |
| 22 | PG | slash | `10/21` | date with slash |
| 23 | PG | alpha | `add` | Snowball snapshot difference (`added` -> `add` on PG, `ad` on ES) |
| 24 | PG | hyphen | `home-field` | hyphen whole |
| 25 | PG | digit | `735` | `$9,735`, comma split |
| 26 | PG | digit | `-2454` | phone number, signed integer |
| 27 | ParadeDB | stopword | `down` | PG stopword, kept by ParadeDB's list |
| 28 | ParadeDB | stopword | `i` | same |
| 29 | PG | alpha | `m` | `I’m` kept whole by ParadeDB; PG splits and keeps `m` |
| 30 | ParadeDB | stopword | `her` | stopword |
| 31 | PG | hyphen | `non-dis` | hyphen whole |
| 32 | ParadeDB | stopword | `between` | stopword |
| 33 | ParadeDB | stopword | `been` | stopword |
| 34 | PG | digit | `13` | `13,700`, comma split |
| 35 | PG | digit | `+8` | signed integer |
| 36 | ParadeDB | stopword | `up` | stopword |
| 37 | ParadeDB | apostrophe | `batsh’` | `Batsh’s` kept whole, Snowball strips `s` and leaves `’` |
| 38 | PG | hyphen | `third-minut` | hyphen whole |
| 39 | ParadeDB | stopword | `those` | stopword |
| 40 | ParadeDB | stopword | `me` | stopword |
| 41 | ParadeDB | alpha | `texanspr` | URL path word; PG keeps URL parts as `url`/`host` tokens |
| 42 | ParadeDB | stopword | `he` | stopword |
| 43 | ParadeDB | stopword | `you` | stopword |
| 44 | PG | digit | `000` | `20,000`, comma split |
| 45 | ParadeDB | stopword | `more` | stopword |
| 46 | ParadeDB | stopword | `who` | stopword |
| 47 | ParadeDB | apostrophe | `we’r` | `We’re` kept whole and stemmed |
| 48 | ParadeDB | stopword | `becaus` | stem of the stopword `because` |
| 49 | ParadeDB | stopword | `between` | stopword |
| 50 | ParadeDB | dotted | `u.` | `U.S.`: Snowball stems `u.s` to `u.`; PG keeps `u.s` unstemmed |
