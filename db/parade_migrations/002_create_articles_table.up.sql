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
    metadata         jsonb       NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_articles_search ON articles
    USING bm25 (id, title, description, content)
    WITH (key_field='id');
CREATE INDEX idx_articles_published_at ON articles (published_at DESC NULLS LAST);
COMMIT;
