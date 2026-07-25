package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestNewRootCmdRegistersEverySubcommand covers main's wiring itself —
// previously untested; a subcommand accidentally left off root.AddCommand
// (a real mistake: every one of these is a manually maintained line) would
// otherwise only be caught by someone noticing `skillci <name>` says
// "unknown command" in the field.
func TestNewRootCmdRegistersEverySubcommand(t *testing.T) {
	want := []string{"init", "check", "eval", "regress", "accept", "badge", "diff", "fuzz", "bisect"}
	root := newRootCmd()
	for _, name := range want {
		if cmd, _, err := root.Find([]string{name}); err != nil || cmd.Name() != name {
			t.Errorf("subcommand %q not registered on root (err = %v)", name, err)
		}
	}
}

// TestNewRootCmdSilencesErrors covers the field that actually fixes the
// double-print: Cobra's default behavior is to print "Error: <err>"
// itself on any RunE/parse error, in addition to main's own
// fmt.Fprintln(os.Stderr, err) — every failing command was printing its
// error message twice (once as "Error: X" from Cobra, once bare from
// main). Caught live: running the real compiled binary against a
// deliberately failing case showed both lines.
func TestNewRootCmdSilencesErrors(t *testing.T) {
	if !newRootCmd().SilenceErrors {
		t.Error("SilenceErrors = false, want true — Cobra would print its own \"Error: ...\" line in addition to main's, duplicating every error message")
	}
}

// TestMainDoesNotDoublePrintErrors is the real end-to-end proof, not just
// a field assertion: builds and runs the actual binary against a
// guaranteed parse error (an unrecognized flag) and checks the error text
// appears in the output exactly once.
func TestMainDoesNotDoublePrintErrors(t *testing.T) {
	bin := t.TempDir() + "/skillci-test-bin"
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build error = %v: %s", err, out)
	}

	cmd := exec.Command(bin, "regress", "--no-such-flag")
	out, _ := cmd.CombinedOutput()
	got := string(out)
	count := strings.Count(got, "unknown flag")
	if count != 1 {
		t.Errorf("output = %q, want \"unknown flag\" to appear exactly once, got %d times", got, count)
	}
}

func TestNewRootCmdHelpRuns(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(--help) error = %v", err)
	}
	if out.Len() == 0 {
		t.Error("--help produced no output")
	}
}

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
