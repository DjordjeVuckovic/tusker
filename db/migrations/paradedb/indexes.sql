CREATE INDEX idx_articles_search ON articles USING bm25 (
    id,
    (title::pdb.simple('stemmer=english', 'stopwords_language=english')),
    (description::pdb.simple('stemmer=english', 'stopwords_language=english')),
    (content::pdb.simple('stemmer=english', 'stopwords_language=english')),
    published_at,
    (publisher_domain::pdb.literal)
) WITH (key_field = 'id', target_segment_count = 1, mutable_segment_rows = 1000);
CREATE INDEX idx_article_embedding ON article_embeddings
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
