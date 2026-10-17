CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
-- BM25 full-text search (TigerData pg_textsearch). The timescale/timescaledb-ha
-- image already preloads it via shared_preload_libraries, so CREATE EXTENSION is enough.
CREATE EXTENSION IF NOT EXISTS pg_textsearch;
