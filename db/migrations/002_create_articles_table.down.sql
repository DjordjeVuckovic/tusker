BEGIN;
DROP TABLE IF EXISTS articles;
DROP INDEX IF EXISTS idx_articles_search_vector;
DROP INDEX IF EXISTS idx_articles_published_at;
COMMIT;