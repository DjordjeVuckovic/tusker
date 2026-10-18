"""Paice's indices on hand-computed cases."""

import math

from stem_eval import paice


def test_one_missed_merge_and_one_wrong_merge():
    # Concepts A = {a1, a2}, B = {b1}. The stemmer splits A and merges a2 with b1.
    # GDMT = 1, GUMT = 0.5 * (1*1 + 1*1) = 1, so UI = 1.
    # GDNT = 0.5*2*1 + 0.5*1*2 = 2, GWMT = 0.5 * (1*1 + 1*1) = 1, so OI = 0.5.
    words = ["a1", "a2", "b1"]
    concept = {"a1": "A", "a2": "A", "b1": "B"}
    stem = {"a1": "x", "a2": "y", "b1": "y"}
    result = paice(words, concept, stem)
    assert math.isclose(result["UI"], 1.0)
    assert math.isclose(result["OI"], 0.5)


def test_perfect_stemmer_scores_zero():
    words = ["run", "runs", "dog", "dogs"]
    concept = {"run": "run", "runs": "run", "dog": "dog", "dogs": "dog"}
    result = paice(words, concept, dict(concept))
    assert result["UI"] == 0.0 and result["OI"] == 0.0


def test_identity_has_full_understemming_and_no_overstemming():
    words = ["run", "runs", "dog"]
    concept = {"run": "run", "runs": "run", "dog": "dog"}
    result = paice(words, concept, {w: w for w in words})
    assert math.isclose(result["UI"], 1.0) and result["OI"] == 0.0


def test_merging_everything_has_no_understemming():
    # Concepts {run, runs} and {dog}; one stem for all. GDNT = 0.5*2*1 + 0.5*1*2 = 2,
    # GWMT = 0.5 * (2*1 + 1*2) = 2, so OI = 1.
    words = ["run", "runs", "dog"]
    concept = {"run": "run", "runs": "run", "dog": "dog"}
    result = paice(words, concept, {w: "z" for w in words})
    assert result["UI"] == 0.0 and math.isclose(result["OI"], 1.0)
