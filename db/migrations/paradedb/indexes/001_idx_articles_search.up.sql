SET maintenance_work_mem = '1GB';
SET max_parallel_maintenance_workers = 4;
CREATE INDEX idx_articles_search ON articles USING bm25 (
    id,
    (title::pdb.simple('stemmer=english', 'stopwords_language=english')),
    (description::pdb.simple('stemmer=english', 'stopwords_language=english')),
    (content::pdb.simple('stemmer=english', 'stopwords_language=english')),
    published_at,
    (publisher_domain::pdb.literal)
) WITH (key_field = 'id');
