CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS pg_trgm;
-- levenshtein for the fuzzy track's edit-distance engine.
CREATE EXTENSION IF NOT EXISTS fuzzystrmatch;
CREATE EXTENSION IF NOT EXISTS vector;

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
    search_vector    tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('english', title), 'A') ||
        setweight(to_tsvector('english', description), 'B') ||
        setweight(to_tsvector('english', content), 'C')
    ) STORED
);
CREATE INDEX idx_articles_published_at ON articles (published_at DESC NULLS LAST);
CREATE INDEX idx_articles_publisher_domain ON articles (publisher_domain);

CREATE TABLE article_embeddings
(
    article_id uuid         NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    model_name text         NOT NULL,
    embedding  vector(1024) NOT NULL,
    PRIMARY KEY (article_id, model_name)
);

CREATE TABLE corpus_load
(
    finished_at timestamptz NOT NULL PRIMARY KEY,
    record      jsonb       NOT NULL
);
