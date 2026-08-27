BEGIN;
-- Rebuilds the vector index at the declared build point. Editing the CREATE
-- INDEX in 004 in place only reaches a fresh database; one that already ran it
-- keeps the graph pgvector built at its own ef_construction = 64, which is the
-- confound this migration exists to remove.
--
-- DROP + CREATE, not ALTER: m and ef_construction are consumed while the graph
-- is assembled, so only a rebuild applies them. No row is read or written —
-- article_embeddings keeps every vector it holds.
DROP INDEX IF EXISTS idx_article_embedding;
CREATE INDEX idx_article_embedding ON article_embeddings USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 100);
COMMIT;
