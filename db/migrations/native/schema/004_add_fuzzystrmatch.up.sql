-- fuzzystrmatch provides levenshtein / levenshtein_less_equal for the
-- news_fuzzy track's pg-levenshtein engine (edit-distance typo correction).
CREATE EXTENSION IF NOT EXISTS fuzzystrmatch;
