BEGIN;
DROP INDEX IF EXISTS idx_articles_source_name;
DROP INDEX IF EXISTS idx_articles_published_at;
DROP INDEX IF EXISTS idx_articles_bm25;
DROP TABLE IF EXISTS articles;
COMMIT;
