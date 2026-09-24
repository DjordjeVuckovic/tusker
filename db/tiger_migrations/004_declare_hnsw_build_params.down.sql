BEGIN;
ALTER INDEX idx_article_embedding RESET (m, ef_construction);
COMMIT;
