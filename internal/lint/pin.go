package lint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// maxPinnedSourceBytes caps how much of a pinned URL's response body
	// is read before hashing — a skill author pins a specific resource,
	// not an open-ended download; this also bounds worst-case CI time and
	// memory for a misbehaving or malicious endpoint.
	maxPinnedSourceBytes = 5 << 20 // 5MB
	pinnedSourceTimeout  = 15 * time.Second
)

// VerifyPinnedSources reads skillPath's frontmatter for pinned_sources
// entries (see PinnedSource) and, for each, fetches the URL over the
// network and compares its content's SHA-256 hash against the declared
// value — implementing OWASP AST02's "pin dependencies to immutable
// hashes" mitigation and catching AST07-style update drift, without
// needing a package registry: skillci itself becomes the trust anchor
// for content the skill's own author already reviewed once and pinned.
//
// This is the ONLY function anywhere in package lint that makes a
// network call. Every other check (LintSkill, LintEvals, and everything
// in security.go, including the AST05 external-instructions detector,
// which only pattern-matches for a URL in the text — it never fetches
// one) is deliberately local-only, matching `skillci check`'s documented
// "no API calls" contract. Callers MUST gate this behind an explicit
// opt-in the user has to ask for by name (e.g. a --verify-pinned-sources
// flag defaulting to false) — never run it as part of a default check.
func VerifyPinnedSources(ctx context.Context, skillPath string) ([]Issue, error) {
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", skillPath, err)
	}

	fm, _, err := splitFrontmatter(string(data))
	if err != nil {
		// Malformed frontmatter is already reported by LintSkill's own
		// invalid-frontmatter path; nothing new to say about it here.
		return nil, nil
	}

	var meta frontmatter
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return nil, nil
	}

	if len(meta.PinnedSources) == 0 {
		return nil, nil
	}

	client := &http.Client{Timeout: pinnedSourceTimeout}
	var issues []Issue
	for _, src := range meta.PinnedSources {
		iss := verifyOnePinnedSource(ctx, client, skillPath, src)
		if iss != nil {
			issues = append(issues, *iss)
		}
	}
	return issues, nil
}

func verifyOnePinnedSource(ctx context.Context, client *http.Client, skillPath string, src PinnedSource) *Issue {
	url := strings.TrimSpace(src.URL)
	wantHash := strings.ToLower(strings.TrimSpace(src.SHA256))

	if url == "" || wantHash == "" {
		return &Issue{File: skillPath, Line: 1, Rule: "ast02-pinned-source-invalid",
			Msg: fmt.Sprintf("pinned_sources entry is missing url or sha256 (url=%q, sha256=%q)", src.URL, src.SHA256)}
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return &Issue{File: skillPath, Line: 1, Rule: "ast02-pinned-source-invalid",
			Msg: fmt.Sprintf("pinned_sources url %q is not http(s) — only http(s) sources can be verified", url)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return &Issue{File: skillPath, Line: 1, Rule: "ast02-pinned-source-unreachable",
			Msg: fmt.Sprintf("pinned source %q: %v", url, err)}
	}
	resp, err := client.Do(req)
	if err != nil {
		return &Issue{File: skillPath, Line: 1, Rule: "ast02-pinned-source-unreachable",
			Msg: fmt.Sprintf("pinned source %q could not be fetched: %v", url, err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &Issue{File: skillPath, Line: 1, Rule: "ast02-pinned-source-unreachable",
			Msg: fmt.Sprintf("pinned source %q returned HTTP %d", url, resp.StatusCode)}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPinnedSourceBytes+1))
	if err != nil {
		return &Issue{File: skillPath, Line: 1, Rule: "ast02-pinned-source-unreachable",
			Msg: fmt.Sprintf("pinned source %q: error reading response body: %v", url, err)}
	}
	if len(body) > maxPinnedSourceBytes {
		return &Issue{File: skillPath, Line: 1, Rule: "ast02-pinned-source-unreachable",
			Msg: fmt.Sprintf("pinned source %q exceeds the %d-byte verification limit", url, maxPinnedSourceBytes)}
	}

	sum := sha256.Sum256(body)
	gotHash := hex.EncodeToString(sum[:])
	if gotHash != wantHash {
		return &Issue{File: skillPath, Line: 1, Rule: "ast02-pinned-source-mismatch",
			Msg: fmt.Sprintf("pinned source %q content hash changed: declared sha256:%s, got sha256:%s — review the change before trusting it", url, wantHash, gotHash)}
	}
	return nil
}
