SET maintenance_work_mem = '1GB';
SET max_parallel_maintenance_workers = 4;
CREATE INDEX idx_articles_bm25 ON articles
    USING bm25 (search_text) WITH (text_config = 'english', k1 = 1.2, b = 0.75);
