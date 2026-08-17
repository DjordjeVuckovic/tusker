BEGIN;
CREATE TABLE articles
(
    id            uuid        NOT NULL PRIMARY KEY DEFAULT uuid_generate_v4(),
    title         text        NOT NULL,
    subtitle      text,
    content       text        NOT NULL,
    author        text                             default ''::text,
    url           text        NOT NULL,
    published_at  timestamptz,
    metadata      jsonb                            DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL             DEFAULT now(),
    language      VARCHAR(10)                      DEFAULT 'english',
    description   text                             DEFAULT '',
    -- pg_textsearch indexes a single text column only (no multi-column, no
    -- expression indexing), so concatenate the searchable fields into a
    -- generated column and index that. See docs "Current limitations".
    search_text   text GENERATED ALWAYS AS (
        coalesce(title, '') || ' ' ||
        coalesce(subtitle, '') || ' ' ||
        coalesce(description, '') || ' ' ||
        coalesce(content, '')
    ) STORED
);
-- BM25 index. text_config drives tokenization/stemming; k1/b use defaults (1.2/0.75).
CREATE INDEX idx_articles_bm25 ON articles
    USING bm25 (search_text)
    WITH (text_config='english');
CREATE INDEX idx_articles_published_at ON articles (published_at DESC NULLS LAST);
COMMIT;
