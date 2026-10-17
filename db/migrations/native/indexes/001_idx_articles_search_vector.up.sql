SET maintenance_work_mem = '1GB';
SET max_parallel_maintenance_workers = 4;
CREATE INDEX idx_articles_search_vector ON articles USING gin (search_vector);
