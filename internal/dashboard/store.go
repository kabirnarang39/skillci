package dashboard

import (
	"context"
	"database/sql"
	_ "embed"
	"time"

	_ "github.com/lib/pq"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	db *sql.DB
}

func NewStore(databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schemaSQL)
	return err
}

type IngestedResult struct {
	Owner, Repo, Skill, CommitSHA, Model string
	Passed                               bool
	Timestamp                            time.Time
}

func (s *Store) InsertResult(ctx context.Context, r IngestedResult) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO results (owner, repo, skill, commit_sha, model, passed, ts) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		r.Owner, r.Repo, r.Skill, r.CommitSHA, r.Model, r.Passed, r.Timestamp)
	return err
}

func (s *Store) SkillHistory(ctx context.Context, owner, repo, skill string) ([]IngestedResult, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT owner, repo, skill, commit_sha, model, passed, ts FROM results
		 WHERE owner=$1 AND repo=$2 AND skill=$3 ORDER BY ts DESC LIMIT 200`,
		owner, repo, skill)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IngestedResult
	for rows.Next() {
		var r IngestedResult
		if err := rows.Scan(&r.Owner, &r.Repo, &r.Skill, &r.CommitSHA, &r.Model, &r.Passed, &r.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type LeaderboardEntry struct {
	Owner, Repo, Skill string
	PassRate           float64
	ModelsCovered      int
	LastRun            time.Time
}

// Leaderboard aggregates, per (owner, repo, skill), the most recent result
// per model, then reports the pass rate across those latest-per-model rows.
func (s *Store) Leaderboard(ctx context.Context) ([]LeaderboardEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (owner, repo, skill, model)
				owner, repo, skill, model, passed, ts
			FROM results
			ORDER BY owner, repo, skill, model, ts DESC
		)
		SELECT owner, repo, skill,
			AVG(CASE WHEN passed THEN 1.0 ELSE 0.0 END) AS pass_rate,
			COUNT(DISTINCT model) AS models_covered,
			MAX(ts) AS last_run
		FROM latest
		GROUP BY owner, repo, skill
		ORDER BY last_run DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LeaderboardEntry
	for rows.Next() {
		var e LeaderboardEntry
		if err := rows.Scan(&e.Owner, &e.Repo, &e.Skill, &e.PassRate, &e.ModelsCovered, &e.LastRun); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
