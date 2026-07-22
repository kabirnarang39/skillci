package snapshot

import "testing"

func TestComputeNoChange(t *testing.T) {
	d := Compute("Old leaves drift and fall.", "Old leaves drift and fall.")
	if d.Changed {
		t.Errorf("Changed = true, want false for identical text")
	}
	if d.WordsUnchanged != 5 {
		t.Errorf("WordsUnchanged = %d, want 5", d.WordsUnchanged)
	}
}

func TestComputeWhitespaceOnlyNoChange(t *testing.T) {
	d := Compute("Old   leaves  drift.", "Old leaves drift.")
	if d.Changed {
		t.Errorf("Changed = true, want false for whitespace-only difference")
	}
}

func TestComputeSingleWordChanged(t *testing.T) {
	d := Compute("Old leaves drift and fall.", "Old leaves drift and settle.")
	if !d.Changed {
		t.Fatal("Changed = false, want true")
	}
	if d.WordsChanged != 2 {
		t.Errorf("WordsChanged = %d, want 2 (one deleted, one inserted)", d.WordsChanged)
	}
	if d.Render == "" {
		t.Error("Render is empty, want a rendered diff")
	}
}

func TestComputeWordAdded(t *testing.T) {
	d := Compute("Leaves drift.", "Leaves slowly drift.")
	if !d.Changed {
		t.Fatal("Changed = false, want true")
	}
	if d.WordsChanged != 1 {
		t.Errorf("WordsChanged = %d, want 1 (one inserted)", d.WordsChanged)
	}
}

func TestComputeWordRemoved(t *testing.T) {
	d := Compute("Leaves slowly drift.", "Leaves drift.")
	if !d.Changed {
		t.Fatal("Changed = false, want true")
	}
	if d.WordsChanged != 1 {
		t.Errorf("WordsChanged = %d, want 1 (one deleted)", d.WordsChanged)
	}
}

func TestComputeEmptyToNonEmpty(t *testing.T) {
	d := Compute("", "Now there is text.")
	if !d.Changed {
		t.Fatal("Changed = false, want true")
	}
	if d.WordsUnchanged != 0 {
		t.Errorf("WordsUnchanged = %d, want 0", d.WordsUnchanged)
	}
}

func TestComputeLongRunCollapsesWithContext(t *testing.T) {
	old := "one two three four five six seven eight nine ten changed eleven twelve thirteen fourteen fifteen"
	new := "one two three four five six seven eight nine ten replaced eleven twelve thirteen fourteen fifteen"
	d := Compute(old, new)
	if !d.Changed {
		t.Fatal("Changed = false, want true")
	}
	if d.Render == old || d.Render == new {
		t.Error("Render should not simply echo the full unchanged text")
	}
}

func TestNormalizeCollapsesWhitespace(t *testing.T) {
	got := Normalize("  Old   leaves\ndrift.  ")
	want := "Old leaves drift."
	if got != want {
		t.Errorf("Normalize() = %q, want %q", got, want)
	}
}
