BEGIN;
DROP INDEX IF EXISTS idx_articles_published_at;
DROP INDEX IF EXISTS idx_articles_search;
DROP TABLE IF EXISTS articles;
COMMIT;
