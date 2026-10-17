BEGIN;
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE article_embeddings
(
    article_id uuid         NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    model_name text         NOT NULL,
    embedding  vector(1024) NOT NULL,
    PRIMARY KEY (article_id, model_name)
);
COMMIT;
