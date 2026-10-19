"""Generate the report tables from the result files, so no number is copied by hand.

Reads out/{manifest,agreement,stem_eval,tiger_check,divergence}.json and the token dumps,
writes out/tables.md and out/results.json.
"""

import json
import statistics

from agreement import FULL_CHAIN, TOKENIZER_ONLY
from common import FIELDS, OUT, read_dump

CLASSES = [
    "hyphen", "contains_digit", "slash_or_url", "stopword", "apostrophe", "dotted_abbreviation",
    "dotted_host", "email", "alpha", "non_latin_script", "latin_diacritic", "symbol_or_emoji",
]


def pct(value: float) -> str:
    return f"{100 * value:.1f}%"


def agreement_table(group: dict, reference: str) -> list[str]:
    rows = [
        f"Mean per-document Jaccard against `{reference}` (exact-match share in brackets); n = 10,000 per field, "
        "units where both sides are empty left out.",
        "",
        "| chain | title | description | content | content vocabulary Jaccard |",
        "|---|---|---|---|---|",
    ]
    for chain, fields in group.items():
        cells = [f"{fields[f]['jaccard_mean']:.4f} ({pct(fields[f]['exact_share'])})" for f in FIELDS]
        rows.append(f"| `{chain}` | " + " | ".join(cells) + f" | {fields['content']['vocab_jaccard']:.4f} |")
    return rows


def class_table(group: dict, chains: list[str], field: str) -> list[str]:
    rows = [
        f"Share of disagreeing (article, term) units by class, `{field}`. "
        "PG-only = terms PG indexes and the chain does not; chain-only = the reverse.",
        "",
        "| chain | side | total units | " + " | ".join(CLASSES) + " |",
        "|---|---|---|" + "---|" * len(CLASSES),
    ]
    for chain in chains:
        for side, label in (("only_in_reference", "PG-only"), ("only_in_candidate", "chain-only")):
            data = group[chain][field][side]
            total = sum(c["units"] for c in data.values())
            cells = [pct(data[c]["units"] / total) if c in data else "0" for c in CLASSES]
            rows.append(f"| `{chain}` | {label} | {total:,} | " + " | ".join(cells) + " |")
    return rows


def tokens_table(chains: list[str]) -> list[str]:
    rows = ["Mean indexed tokens per article (distinct positions), sample A.", "", "| chain | title | description | content |", "|---|---|---|---|"]
    for chain in chains:
        dump = read_dump(chain)
        cells = [f"{statistics.fmean(r['n'] for (_, f), r in dump.items() if f == field):.1f}" for field in FIELDS]
        rows.append(f"| `{chain}` | " + " | ".join(cells) + " |")
    return rows


def main() -> None:
    manifest = json.loads((OUT / "manifest.json").read_text())
    agreement = json.loads((OUT / "agreement.json").read_text())
    stems = json.loads((OUT / "stem_eval.json").read_text())
    tiger = json.loads((OUT / "tiger_check.json").read_text())
    divergence = json.loads((OUT / "divergence.json").read_text())

    lines = ["# Generated tables", "", f"Source sha256 `{manifest['source_sha256']}`, seed {manifest['seed']}, "
             f"sample A {manifest['sample_a']['rows']:,} (`{manifest['sample_a']['sha256'][:12]}`), "
             f"sample B {manifest['sample_b']['rows']:,} (`{manifest['sample_b']['sha256'][:12]}`).", ""]

    lines += ["## T1. Stem identity, one word at a time", "",
              f"Vocabulary: {stems['m1_identity'][0]['words']:,} lowercase ASCII-alphabetic words of sample A. "
              "Share weighted by document frequency over (article, field) units.", "",
              "| left | right | words that differ | df-weighted share | most frequent differing words (word, left, right) |",
              "|---|---|---|---|---|"]
    for row in stems["m1_identity"]:
        examples = ", ".join(f"{w}: {a}/{b}" for w, a, b, _ in row["examples"][:5])
        lines.append(f"| `{row['left']}` | `{row['right']}` | {row['differ']:,} | {100*row['differ_df_share']:.3f}% | {examples} |")
    lines += ["", f"hunspell, same dictionary file, PG against ES: {stems['m1_hunspell_pg_vs_es_differ']:,} words with different "
              f"lemma sets; words with more than one lemma: PG {stems['m1_hunspell_multi_output_words']['pg.hunspell']:,}, "
              f"ES {stems['m1_hunspell_multi_output_words']['es.hunspell']:,}.", ""]

    lines += ["## T2. Tiger against PG `english`", "", "| field | docs | total length | distinct terms | words checked | mismatches |", "|---|---|---|---|---|---|"]
    for field, v in tiger.items():
        t, p = v["tiger"], v["pg"]
        same = lambda a, b: f"{a:,}" if a == b else f"tiger {a:,} / PG {b:,}"
        lines.append(f"| {field} | {same(t['total_docs'], p['docs_with_terms'])} | {same(t['total_len'], p['total_len'])} | "
                     f"{same(t['terms'], p['terms'])} | {v['words_checked']} | {len(v['word_mismatches'])} |")
    lines.append("")

    lines += ["## T3. Tokenizer only", ""] + agreement_table(agreement["tokenizer_only"], "pg.simple") + [""]
    lines += ["## T4. Full chain", ""] + agreement_table(agreement["full_chain"], "pg.english") + [""]
    lines += ["## T5. What the disagreement is made of", ""]
    lines += class_table(agreement["tokenizer_only"], ["es.standard", "es.standard+apos", "pdb.unicode_words", "pdb.simple"], "content") + [""]
    lines += class_table(agreement["full_chain"], ["es.english", "es.standard+apos+pgstop+snowball", "pdb.unicode_words+stem+stop"], "content") + [""]

    lines += ["## T6. Stemmer character", "",
              f"Words with document frequency >= {stems['m4_min_df']} that spaCy also saw: {stems['m4_words']:,}. "
              f"spaCy gave {stems['m4_spacy_words_with_several_lemmas']:,} of them more than one lemma in context; the most "
              "frequent is used. Lower UI = fewer missed merges, lower OI = fewer wrong merges. OI values are small because "
              "the denominator counts every pair of words from different concepts.", "",
              "| stemmer | classes | vocabulary reduction | UI vs spaCy | OI vs spaCy | UI vs WordNet | OI vs WordNet |",
              "|---|---|---|---|---|---|---|"]
    for name, e in stems["m4"].items():
        sp, wn = e["vs_spacy"], e["vs_wordnet"]
        lines.append(f"| {name} | {e['classes']:,} | {pct(e['reduction'])} | {sp['UI']:.3f} | {sp['OI']:.2e} | {wn['UI']:.3f} | {wn['OI']:.2e} |")
    lines.append("")

    lines += ["## T7. Retrieval divergence (proxy queries, sample B)", "",
              f"{divergence['queries']} queries. Zero-hit queries per chain: "
              + ", ".join(f"`{c}` {n}" for c, n in divergence["zero_hits"].items()) + ".", "",
              "| engine | left | right | top-10 Jaccard (n) | identical top 10 | RBO ext, p=0.9 (n) |", "|---|---|---|---|---|---|"]
    for engine in ("es_pairs", "pg_pairs"):
        for r in divergence[engine]:
            lines.append(f"| {engine[:2]} | `{r['left']}` | `{r['right']}` | {r['top10_jaccard_mean']:.3f} ({r['queries_both_nonempty']}) | "
                         f"{pct(r['identical_top10_share'])} | {r['rbo_ext_mean']:.3f} ({r['queries_both_full']}) |")
    lines.append("")

    cost = divergence["cost"]
    lines += ["## T8. Index size, sample B", "", "ES: bytes of the chain's three sub-fields after force-merge to one segment (`_disk_usage`).", "",
              "| chain | ES bytes |", "|---|---|"]
    lines += [f"| `{c}` | {b:,} |" for c, b in cost["es_field_bytes"].items()]
    lines += ["", "PG: all columns written in one pass, indexes built afterwards, then VACUUM ANALYZE.", "",
              "| config | tsvector bytes | GIN bytes |", "|---|---|---|"]
    lines += [f"| `{c}` | {v['tsvector_bytes']:,} | {v['gin_bytes']:,} |" for c, v in cost["pg"].items()]
    lines += ["", "## T9. Tokens per article", ""] + tokens_table(["pg.english", "pg.simple"] + FULL_CHAIN) + [""]

    direct = json.loads((OUT / "es_vs_pdb.json").read_text())
    lines += ["## T10. ES and ParadeDB compared directly", "", "| pair | title | description | content |", "|---|---|---|---|"]
    for pair, fields in direct.items():
        if pair == "stopwords":
            continue
        lines.append(f"| `{pair}` | " + " | ".join(f"{fields[f]['jaccard_mean']:.6f}" for f in FIELDS) + " |")
    stop = direct["stopwords"]
    lines += ["", f"Fed PG's 127 stopwords, Lucene `_english_` drops {len(stop['lucene_drops_of_pg127'])} and ParadeDB "
              f"`stopwords_language=english` drops {len(stop['paradedb_drops_of_pg127'])}; the two dropped sets are identical: {stop['identical']}.", ""]
    index_check = json.loads((OUT / "pdb_index_check.json").read_text())
    checked = sum(v["terms_checked"] for v in index_check.values())
    missed = [m | {"chain_field": k} for k, v in index_check.items() for m in v["mismatches"]]
    lines += ["## T11. ParadeDB cast output against its bm25 index", "",
              f"{checked:,} terms over {len(index_check)} (chain, field) aliases; mismatching match counts: {len(missed)}."]
    lines += [f"- `{m['chain_field']}` `{m['term']}`: index {m['index']}, cast {m['cast']}" for m in missed]
    lines.append("")

    (OUT / "tables.md").write_text("\n".join(lines) + "\n")
    (OUT / "results.json").write_text(json.dumps({
        "manifest": manifest, "stem_eval": stems, "tiger": tiger, "divergence": divergence,
        "agreement_summary": {g: {c: {f: {k: v[k] for k in ("jaccard_mean", "jaccard_median", "jaccard_p5", "exact_share", "vocab_jaccard", "units", "units_both_empty")} for f, v in fs.items()} for c, fs in agreement[g].items()} for g in agreement},
    }, indent=1, ensure_ascii=False))
    print("wrote tables.md")


if __name__ == "__main__":
    main()
