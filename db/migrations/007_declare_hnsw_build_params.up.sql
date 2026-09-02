BEGIN;
-- Records the build parameters on an index that already exists. Editing the
-- CREATE INDEX in 004 in place only reaches a fresh database; one that already
-- ran it keeps an index whose pg_class.reloptions is empty, so nothing can be
-- asked what the graph was built with.
--
-- SET rather than DROP + CREATE because the declared values are the ones
-- pgvector already built this graph at: it is written down, not changed, and no
-- rebuild is needed. That equivalence is what makes this migration cheap, and it
-- holds only while the declared value stays pgvector's default — moving it to
-- another value means rebuilding the index, not altering it.
ALTER INDEX idx_article_embedding SET (m = 16, ef_construction = 64);
COMMIT;
