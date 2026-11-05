-- Jane's own job crawler: employer job boards it reads, and the jobs it has
-- saved from them, so searches don't depend on a paid job feed.

CREATE TABLE IF NOT EXISTS job_boards (
    ats TEXT NOT NULL,                 -- greenhouse, lever, ashby, workday
    slug TEXT NOT NULL,                -- board id; for Workday "host/tenant/site"
    company TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    last_crawled_at TIMESTAMPTZ,
    last_status TEXT NOT NULL DEFAULT '',
    job_count INTEGER NOT NULL DEFAULT 0,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (ats, slug)
);

CREATE INDEX IF NOT EXISTS job_boards_due_idx
    ON job_boards (active, last_crawled_at NULLS FIRST);

CREATE TABLE IF NOT EXISTS crawled_jobs (
    id TEXT PRIMARY KEY,               -- "<ats>:<slug>:<job id>"
    ats TEXT NOT NULL,
    board_slug TEXT NOT NULL,
    company TEXT NOT NULL,
    title TEXT NOT NULL,
    title_search TEXT NOT NULL,        -- lowercased title for searching
    location TEXT NOT NULL DEFAULT '',
    country_code TEXT NOT NULL DEFAULT '',
    work_arrangement TEXT NOT NULL DEFAULT '',
    employment_type TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    apply_url TEXT NOT NULL,
    posted_at TIMESTAMPTZ,
    salary_min DOUBLE PRECISION,
    salary_max DOUBLE PRECISION,
    salary_currency TEXT NOT NULL DEFAULT '',
    salary_period TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT TRUE,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS crawled_jobs_board_idx
    ON crawled_jobs (ats, board_slug);

CREATE INDEX IF NOT EXISTS crawled_jobs_active_idx
    ON crawled_jobs (active, last_seen_at DESC);
