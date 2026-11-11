-- Searches find saved jobs by title words (title_search ~ '\m(nurse|rn)').
-- Without an index Postgres reads every saved job for each search, which
-- took several seconds. A trigram index lets it jump to matching titles.
-- If the pg_trgm extension isn't available, searches still work (slower)
-- and the server still starts.
DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_trgm;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'pg_trgm is not available: %', SQLERRM;
END
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm') THEN
        CREATE INDEX IF NOT EXISTS crawled_jobs_title_trgm_idx
            ON crawled_jobs USING gin (title_search gin_trgm_ops)
            WHERE active;
    END IF;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'title index not created: %', SQLERRM;
END
$$;

ANALYZE crawled_jobs;
