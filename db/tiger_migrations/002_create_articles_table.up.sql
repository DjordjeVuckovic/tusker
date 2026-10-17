BEGIN;
CREATE TABLE articles
(
    id               uuid        NOT NULL PRIMARY KEY,
    url              text        NOT NULL,
    title            text        NOT NULL,
    description      text        NOT NULL DEFAULT '',
    content          text        NOT NULL,
    published_at     timestamptz NULL,
    language         text        NOT NULL,
    publisher_domain text        NOT NULL DEFAULT '',
    metadata         jsonb       NOT NULL DEFAULT '{}',
    -- pg_textsearch indexes a single text column only (no multi-column, no
    -- expression indexing), so concatenate the searchable fields into a
    -- generated column and index that. See docs "Current limitations".
    search_text      text GENERATED ALWAYS AS (
        title || ' ' || description || ' ' || content
    ) STORED
);
-- BM25 index. text_config drives tokenization/stemming; k1/b use defaults (1.2/0.75).
CREATE INDEX idx_articles_bm25 ON articles
    USING bm25 (search_text)
    WITH (text_config='english');
CREATE INDEX idx_articles_published_at ON articles (published_at DESC NULLS LAST);
COMMIT;
