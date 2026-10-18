CREATE INDEX idx_articles_title_desc_trgm ON articles
    USING gin ((title || ' ' || coalesce(description, '')) gin_trgm_ops);
