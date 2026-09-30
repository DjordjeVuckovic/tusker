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
    description   text                             DEFAULT ''
);
-- Text fields stem with Snowball English, as the english regconfig does on
-- pg-native and tiger. ParadeDB takes no custom stopword list, so its english
-- list stays smaller than PostgreSQL's. published_at and metadata sit in the
-- index so structured filters are answered by the bm25 scan; metadata is a
-- literal so metadata->>'sourceName' = ... pushes down as an exact term.
CREATE INDEX idx_articles_search ON articles
    USING bm25 (
        id,
        (title::pdb.simple('stemmer=english', 'stopwords_language=english')),
        (subtitle::pdb.simple('stemmer=english', 'stopwords_language=english')),
        (content::pdb.simple('stemmer=english', 'stopwords_language=english')),
        (description::pdb.simple('stemmer=english', 'stopwords_language=english')),
        published_at,
        (metadata::pdb.literal)
    )
    WITH (key_field='id');
CREATE INDEX idx_articles_published_at ON articles (published_at DESC NULLS LAST);
CREATE INDEX idx_articles_source_name ON articles ((metadata->>'sourceName'));
COMMIT;