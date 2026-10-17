SET maintenance_work_mem = '1GB';
SET max_parallel_maintenance_workers = 4;
CREATE INDEX idx_articles_published_at ON articles (published_at DESC NULLS LAST);
