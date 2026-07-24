package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/kabirnarang39/skillci/internal/regress"
)

// printDimensionRollup groups outcomes by each Dimensions key/value pair
// their Case carries and prints a per-group pass count, only listing
// individual cases for groups with at least one failure — mirroring the
// terseness of the main per-outcome list above it. A case with multiple
// dimension keys appears under every group it belongs to; this is a
// display aid only, not a second source of truth for pass/fail data.
// Prints nothing at all when no outcome's Case has any Dimensions set, so
// skills not using this feature see zero output change.
func printDimensionRollup(w io.Writer, outcomes []regress.Outcome) {
	type group struct {
		key, value string
	}
	members := make(map[group][]regress.Outcome)
	for _, o := range outcomes {
		for k, v := range o.Case.Dimensions {
			g := group{key: k, value: v}
			members[g] = append(members[g], o)
		}
	}
	if len(members) == 0 {
		return
	}

	var groups []group
	for g := range members {
		groups = append(groups, g)
	}
	// Sorted iteration: map order is nondeterministic in Go, and this
	// output must be stable run-to-run, not shuffled by chance.
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].key != groups[j].key {
			return groups[i].key < groups[j].key
		}
		return groups[i].value < groups[j].value
	})

	fmt.Fprintln(w, "--- by dimension ---")
	for _, g := range groups {
		groupOutcomes := members[g]
		passed := 0
		for _, o := range groupOutcomes {
			if o.Result.Passed {
				passed++
			}
		}
		fmt.Fprintf(w, "%s=%s: %d/%d passed\n", g.key, g.value, passed, len(groupOutcomes))
		if passed == len(groupOutcomes) {
			continue
		}
		for _, o := range groupOutcomes {
			if o.Result.Passed {
				continue
			}
			strictMarker := ""
			if o.StrictDimensionFail {
				strictMarker = "[STRICT] "
			}
			fmt.Fprintf(w, "  %s[FAIL] %s (%s)\n", strictMarker, o.Case.Name, o.Model)
		}
	}
}
