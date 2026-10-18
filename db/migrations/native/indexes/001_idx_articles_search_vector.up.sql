CREATE INDEX idx_articles_search_vector ON articles USING gin (search_vector);
