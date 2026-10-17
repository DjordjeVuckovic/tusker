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
COMMIT;
