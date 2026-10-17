SET maintenance_work_mem = '1GB';
SET max_parallel_maintenance_workers = 4;
CREATE INDEX idx_articles_title_desc_trgm ON articles
    USING gin ((title || ' ' || coalesce(description, '')) gin_trgm_ops);
