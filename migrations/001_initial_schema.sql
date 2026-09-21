BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Users are keyed independently from Apple so additional auth providers can be
-- added later without changing every foreign key.
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    apple_subject TEXT UNIQUE,
    email TEXT,
    display_name TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE career_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    headline TEXT,
    summary TEXT,
    target_role TEXT,
    location TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Structured Career Profile evidence. entry_index preserves user-facing order
-- and gives the app a stable way to display "Experience entry 2", etc.
CREATE TABLE career_profile_evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    career_profile_id UUID NOT NULL REFERENCES career_profiles(id) ON DELETE CASCADE,
    category TEXT NOT NULL CHECK (
        category IN (
            'skill',
            'experience',
            'education',
            'certification',
            'license',
            'project',
            'summary'
        )
    ),
    entry_index INTEGER NOT NULL DEFAULT 0 CHECK (entry_index >= 0),
    text TEXT NOT NULL CHECK (BTRIM(text) <> ''),
    source TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (career_profile_id, category, entry_index, text)
);

CREATE INDEX career_profile_evidence_profile_category_idx
    ON career_profile_evidence (career_profile_id, category, entry_index);

CREATE TABLE resumes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    content_type TEXT,
    storage_key TEXT,
    extracted_text TEXT,
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX resumes_one_primary_per_user_idx
    ON resumes (user_id)
    WHERE is_primary = TRUE;

CREATE TABLE jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source TEXT NOT NULL,
    source_job_id TEXT,
    title TEXT NOT NULL,
    company TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    country_code TEXT,
    work_arrangement TEXT,
    employment_type TEXT,
    salary_min NUMERIC,
    salary_max NUMERIC,
    salary_currency TEXT,
    salary_period TEXT,
    posted_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    apply_url TEXT NOT NULL DEFAULT '',
    source_url TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (salary_min IS NULL OR salary_min >= 0),
    CHECK (salary_max IS NULL OR salary_max >= 0),
    CHECK (
        salary_min IS NULL OR
        salary_max IS NULL OR
        salary_max >= salary_min
    )
);

CREATE UNIQUE INDEX jobs_source_identity_idx
    ON jobs (source, source_job_id)
    WHERE source_job_id IS NOT NULL AND BTRIM(source_job_id) <> '';

CREATE INDEX jobs_active_posted_idx
    ON jobs (is_active, posted_at DESC);

CREATE INDEX jobs_title_company_idx
    ON jobs (title, company);

CREATE TABLE job_requirements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position >= 0),
    text TEXT NOT NULL CHECK (BTRIM(text) <> ''),
    category TEXT NOT NULL CHECK (
        category IN (
            'skill',
            'experience',
            'education',
            'license',
            'certification',
            'physical',
            'travel',
            'other'
        )
    ),
    importance TEXT NOT NULL CHECK (
        importance IN ('required', 'preferred')
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_id, position)
);

CREATE INDEX job_requirements_job_idx
    ON job_requirements (job_id, position);

CREATE TABLE match_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    career_profile_id UUID REFERENCES career_profiles(id) ON DELETE SET NULL,
    resume_id UUID REFERENCES resumes(id) ON DELETE SET NULL,
    match_percentage INTEGER NOT NULL CHECK (
        match_percentage BETWEEN 0 AND 100
    ),
    match_level TEXT NOT NULL CHECK (
        match_level IN (
            'Best Fit',
            'Good Fit',
            'Reach',
            'Insufficient Evidence'
        )
    ),
    explanation TEXT NOT NULL DEFAULT '',
    matcher_version TEXT NOT NULL DEFAULT 'v1',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX match_results_user_created_idx
    ON match_results (user_id, created_at DESC);

CREATE INDEX match_results_user_job_idx
    ON match_results (user_id, job_id, created_at DESC);

-- One row per requirement evaluation. This is the persisted Your Fit result.
CREATE TABLE requirement_match_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    match_result_id UUID NOT NULL REFERENCES match_results(id) ON DELETE CASCADE,
    job_requirement_id UUID NOT NULL REFERENCES job_requirements(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (
        status IN ('supported', 'partial', 'missing')
    ),
    score NUMERIC(4,3) NOT NULL CHECK (
        score >= 0 AND score <= 1
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (match_result_id, job_requirement_id)
);

CREATE INDEX requirement_match_results_match_idx
    ON requirement_match_results (match_result_id);

-- Evidence provenance is stored separately because a requirement can be
-- supported by multiple pieces of candidate evidence.
CREATE TABLE requirement_match_evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requirement_match_result_id UUID NOT NULL
        REFERENCES requirement_match_results(id) ON DELETE CASCADE,
    career_profile_evidence_id UUID
        REFERENCES career_profile_evidence(id) ON DELETE SET NULL,
    resume_id UUID
        REFERENCES resumes(id) ON DELETE SET NULL,
    evidence_text TEXT NOT NULL CHECK (BTRIM(evidence_text) <> ''),
    evidence_category TEXT,
    evidence_source TEXT,
    retrieval_rank INTEGER CHECK (
        retrieval_rank IS NULL OR retrieval_rank > 0
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX requirement_match_evidence_result_idx
    ON requirement_match_evidence (
        requirement_match_result_id,
        retrieval_rank
    );

CREATE TABLE saved_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, job_id)
);

CREATE INDEX saved_jobs_user_created_idx
    ON saved_jobs (user_id, created_at DESC);

CREATE TABLE applications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'saved' CHECK (
        status IN (
            'saved',
            'applied',
            'interviewing',
            'offer',
            'rejected',
            'withdrawn'
        )
    ),
    applied_at TIMESTAMPTZ,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, job_id)
);

CREATE INDEX applications_user_status_idx
    ON applications (user_id, status, updated_at DESC);

COMMIT;
