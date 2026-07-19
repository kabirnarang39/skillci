CREATE TABLE IF NOT EXISTS results (
    id          BIGSERIAL PRIMARY KEY,
    owner       TEXT NOT NULL,
    repo        TEXT NOT NULL,
    skill       TEXT NOT NULL,
    commit_sha  TEXT NOT NULL,
    model       TEXT NOT NULL,
    passed      BOOLEAN NOT NULL,
    ts          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_results_skill ON results (owner, repo, skill, ts DESC);
