SET pg_textsearch.segments_per_level = 8;
SET pg_textsearch.memtable_spill_threshold = 32000000;
CREATE INDEX idx_articles_bm25 ON articles
    USING bm25 (search_text) WITH (text_config = 'english', k1 = 1.2, b = 0.75);
