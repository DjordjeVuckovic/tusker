BEGIN;
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE article_embeddings
(
    id         uuid         NOT NULL PRIMARY KEY DEFAULT uuid_generate_v4(),
    article_id uuid         NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    embedding  VECTOR(1024) NOT NULL,
    model_name VARCHAR(100) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE          DEFAULT now(),
    UNIQUE (article_id, model_name)
);
CREATE INDEX idx_article_embedding ON article_embeddings USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);
COMMIT;
