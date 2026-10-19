CREATE INDEX idx_articles_search_vector ON articles USING gin (search_vector);
CREATE INDEX idx_articles_title_trgm ON articles USING gin (title gin_trgm_ops);
CREATE INDEX idx_articles_title_desc_trgm ON articles
    USING gin ((title || ' ' || coalesce(description, '')) gin_trgm_ops);
CREATE INDEX idx_article_embedding ON article_embeddings
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
