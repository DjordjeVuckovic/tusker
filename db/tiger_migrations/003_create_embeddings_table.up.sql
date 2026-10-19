BEGIN;
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE article_embeddings
(
    article_id uuid         NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    model_name text         NOT NULL,
    embedding  vector(1024) NOT NULL,
    PRIMARY KEY (article_id, model_name)
);
CREATE INDEX idx_article_embedding ON article_embeddings USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);
COMMIT;
