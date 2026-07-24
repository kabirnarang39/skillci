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
