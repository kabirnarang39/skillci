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
