package main

import "testing"

// TestCommandsSilenceUsageOnError covers every subcommand whose RunE error
// represents a normal, expected outcome (a regression detected, a failing
// case, no pending snapshot to promote, ...) rather than misuse of the
// command's own flags/args. Cobra's default behavior prints the full
// flags/usage block on any RunE error — check.go already opted out of
// this for exactly this reason (a lint failure isn't user error), but the
// same reasoning applies to every other command here just as much: in CI,
// a `regress` failure is the single most common outcome the tool exists
// to report, and dumping a wall of flag help on every one of them is pure
// noise a human then has to scroll past to find the actual failure.
func TestCommandsSilenceUsageOnError(t *testing.T) {
	cmds := map[string]bool{ // name -> SilenceUsage
		"init":    newInitCmd().SilenceUsage,
		"check":   newCheckCmd().SilenceUsage,
		"eval":    newEvalCmd().SilenceUsage,
		"regress": newRegressCmd().SilenceUsage,
		"accept":  newAcceptCmd().SilenceUsage,
		"badge":   newBadgeCmd().SilenceUsage,
		"diff":    newDiffCmd().SilenceUsage,
		"fuzz":    newFuzzCmd().SilenceUsage,
		"bisect":  newBisectCmd().SilenceUsage,
	}
	for name, silenced := range cmds {
		if !silenced {
			t.Errorf("%s: SilenceUsage = false, want true — an expected-outcome error shouldn't dump the flags/usage block", name)
		}
	}
}
