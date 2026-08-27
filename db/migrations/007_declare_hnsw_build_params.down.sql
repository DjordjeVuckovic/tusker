BEGIN;
DROP INDEX IF EXISTS idx_article_embedding;
CREATE INDEX idx_article_embedding ON article_embeddings USING hnsw (embedding vector_cosine_ops);
COMMIT;
