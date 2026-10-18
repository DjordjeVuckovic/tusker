CREATE INDEX idx_articles_title_trgm ON articles USING gin (title gin_trgm_ops);
