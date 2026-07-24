// Package bisect implements a binary search over an ordered list of commit
// SHAs to find the earliest one where a caller-supplied test starts
// failing. It has no knowledge of git, models, or eval cases — the caller
// supplies a closure that does whatever historical-state setup and
// evaluation is needed to answer pass/fail for a given SHA.
package bisect

import "fmt"

// Search finds the leftmost (earliest, since candidates is ordered oldest
// first) SHA in candidates for which test returns passed == false. It
// assumes test's results are monotonic across candidates — every commit
// before the culprit passes, every commit from the culprit onward fails —
// the same assumption real `git bisect` makes. It calls test at most
// ceil(log2(len(candidates))) times.
func Search(candidates []string, test func(sha string) (passed bool, err error)) (string, error) {
	if len(candidates) == 0 {
		return "", fmt.Errorf("bisect: no candidates to search")
	}

	lo, hi := 0, len(candidates)-1
	culprit := candidates[len(candidates)-1]
	for lo <= hi {
		mid := lo + (hi-lo)/2
		passed, err := test(candidates[mid])
		if err != nil {
			return "", err
		}
		if passed {
			lo = mid + 1
		} else {
			culprit = candidates[mid]
			hi = mid - 1
		}
	}
	return culprit, nil
}

// SearchLinear scans every candidate in order (unlike Search's binary
// search) and identifies every point where test's result transitions from
// passed to failed. Real git history containing merge commits can violate
// the strict monotonicity Search assumes — a topological (not strictly
// chronological) ordering can interleave commits from different branches,
// so more than one such transition is genuinely possible; unlike Search,
// this makes no assumption about how many there are. The first
// transition's commit is returned as the primary culprit — matching
// Search's answer for the common, genuinely-monotonic case — and any
// further transitions are returned separately so the caller can report
// the ambiguity instead of silently picking one.
func SearchLinear(candidates []string, test func(sha string) (passed bool, err error)) (culprit string, additional []string, err error) {
	if len(candidates) == 0 {
		return "", nil, fmt.Errorf("bisect: no candidates to search")
	}

	prevPassed := true // the caller's already-verified good endpoint, implicitly before candidates[0]
	var transitions []string
	for _, sha := range candidates {
		passed, terr := test(sha)
		if terr != nil {
			return "", nil, terr
		}
		if prevPassed && !passed {
			transitions = append(transitions, sha)
		}
		prevPassed = passed
	}
	if len(transitions) == 0 {
		return "", nil, fmt.Errorf("bisect: no failing candidate found in range")
	}
	return transitions[0], transitions[1:], nil
}
