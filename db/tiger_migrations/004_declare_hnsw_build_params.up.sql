BEGIN;
ALTER INDEX idx_article_embedding SET (m = 16, ef_construction = 64);
COMMIT;
