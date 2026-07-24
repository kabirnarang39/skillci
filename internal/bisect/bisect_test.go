package bisect

import "testing"

var errTestFailure = &testError{"simulated test failure"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// monotonicTest returns a test closure asserting: candidates before
// boundaryIndex pass, candidates from boundaryIndex onward fail — the same
// monotonicity assumption real `git bisect` makes.
func monotonicTest(t *testing.T, candidates []string, boundaryIndex int) func(string) (bool, error) {
	return func(sha string) (bool, error) {
		for i, c := range candidates {
			if c == sha {
				return i < boundaryIndex, nil
			}
		}
		t.Fatalf("test called with unknown sha %q", sha)
		return false, nil
	}
}

func TestSearchFirstCandidateIsCulprit(t *testing.T) {
	candidates := []string{"c1", "c2", "c3", "c4"}
	culprit, err := Search(candidates, monotonicTest(t, candidates, 0))
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if culprit != "c1" {
		t.Errorf("Search() = %q, want %q", culprit, "c1")
	}
}

func TestSearchMidHistoryCulprit(t *testing.T) {
	candidates := []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7"}
	culprit, err := Search(candidates, monotonicTest(t, candidates, 3))
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if culprit != "c4" {
		t.Errorf("Search() = %q, want %q", culprit, "c4")
	}
}

func TestSearchCulpritIsLastCandidate(t *testing.T) {
	candidates := []string{"c1", "c2", "c3", "c4", "c5"}
	culprit, err := Search(candidates, monotonicTest(t, candidates, 4))
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if culprit != "c5" {
		t.Errorf("Search() = %q, want %q", culprit, "c5")
	}
}

func TestSearchSingleCandidate(t *testing.T) {
	candidates := []string{"c1"}
	culprit, err := Search(candidates, monotonicTest(t, candidates, 0))
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if culprit != "c1" {
		t.Errorf("Search() = %q, want %q", culprit, "c1")
	}
}

func TestSearchEmptyCandidatesReturnsError(t *testing.T) {
	if _, err := Search(nil, func(string) (bool, error) { return false, nil }); err == nil {
		t.Error("Search() error = nil, want an error for an empty candidate list")
	}
}

func TestSearchPropagatesTestError(t *testing.T) {
	candidates := []string{"c1", "c2", "c3"}
	wantErr := errTestFailure
	culprit, err := Search(candidates, func(string) (bool, error) { return false, wantErr })
	if err != wantErr {
		t.Errorf("Search() error = %v, want %v", err, wantErr)
	}
	if culprit != "" {
		t.Errorf("Search() culprit = %q, want empty on error", culprit)
	}
}

func TestSearchLinearMatchesSearchOnMonotonicHistory(t *testing.T) {
	candidates := []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7"}
	culprit, additional, err := SearchLinear(candidates, monotonicTest(t, candidates, 3))
	if err != nil {
		t.Fatalf("SearchLinear() error = %v", err)
	}
	if culprit != "c4" {
		t.Errorf("SearchLinear() culprit = %q, want %q (same answer Search gives for this monotonic case)", culprit, "c4")
	}
	if len(additional) != 0 {
		t.Errorf("additional = %v, want none — a genuinely monotonic history has exactly one transition", additional)
	}
}

func TestSearchLinearFindsMultipleTransitionsOnNonMonotonicHistory(t *testing.T) {
	// Simulates the non-linear-history case a merge commit can produce:
	// pass, FAIL (first culprit), pass again, FAIL again (a second,
	// disjoint culprit) — something Search's binary search assumption
	// cannot represent at all, but a real topological ordering can.
	results := map[string]bool{
		"c1": true, "c2": false, "c3": true, "c4": false, "c5": false,
	}
	candidates := []string{"c1", "c2", "c3", "c4", "c5"}
	test := func(sha string) (bool, error) { return results[sha], nil }

	culprit, additional, err := SearchLinear(candidates, test)
	if err != nil {
		t.Fatalf("SearchLinear() error = %v", err)
	}
	if culprit != "c2" {
		t.Errorf("culprit = %q, want %q (the first pass-to-fail transition)", culprit, "c2")
	}
	if len(additional) != 1 || additional[0] != "c4" {
		t.Errorf("additional = %v, want [c4] (the second, disjoint transition — c5 continues the same failing run, not a new transition)", additional)
	}
}

func TestSearchLinearCulpritIsFirstCandidate(t *testing.T) {
	candidates := []string{"c1", "c2", "c3"}
	culprit, additional, err := SearchLinear(candidates, monotonicTest(t, candidates, 0))
	if err != nil {
		t.Fatalf("SearchLinear() error = %v", err)
	}
	if culprit != "c1" {
		t.Errorf("culprit = %q, want %q", culprit, "c1")
	}
	if len(additional) != 0 {
		t.Errorf("additional = %v, want none", additional)
	}
}

func TestSearchLinearEmptyCandidatesReturnsError(t *testing.T) {
	if _, _, err := SearchLinear(nil, func(string) (bool, error) { return false, nil }); err == nil {
		t.Error("SearchLinear() error = nil, want an error for an empty candidate list")
	}
}

func TestSearchLinearPropagatesTestError(t *testing.T) {
	candidates := []string{"c1", "c2", "c3"}
	wantErr := errTestFailure
	culprit, additional, err := SearchLinear(candidates, func(string) (bool, error) { return false, wantErr })
	if err != wantErr {
		t.Errorf("SearchLinear() error = %v, want %v", err, wantErr)
	}
	if culprit != "" || additional != nil {
		t.Errorf("SearchLinear() = %q, %v, want empty/nil on error", culprit, additional)
	}
}

func TestSearchLinearErrorsWhenNothingFails(t *testing.T) {
	candidates := []string{"c1", "c2", "c3"}
	_, _, err := SearchLinear(candidates, func(string) (bool, error) { return true, nil })
	if err == nil {
		t.Error("SearchLinear() error = nil, want an error when no candidate ever fails")
	}
}
