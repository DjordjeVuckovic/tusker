BEGIN;
CREATE TABLE articles
(
    id            uuid        NOT NULL PRIMARY KEY DEFAULT uuid_generate_v4(),
    title         text        NOT NULL,
    subtitle      text,
    content       text        NOT NULL,
    author        text                             default ''::text,
    search_vector tsvector,
    url           text        NOT NULL,
    published_at  timestamptz,
    metadata      jsonb                            DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL             DEFAULT now(),
    language      VARCHAR(10)                      DEFAULT 'english',
    description   text                             DEFAULT ''
);
CREATE INDEX idx_articles_search_vector ON articles
    USING gin (search_vector);
CREATE INDEX idx_articles_published_at ON articles (published_at DESC NULLS LAST);
COMMIT;