package snapshot

import (
	"fmt"
	"strings"
)

type opKind int

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

type op struct {
	kind opKind
	word string
}

// Diff is a word-level comparison between two texts.
type Diff struct {
	Changed        bool
	Render         string
	WordsChanged   int
	WordsUnchanged int
}

// Normalize trims and collapses whitespace so purely cosmetic whitespace
// differences never register as a content change.
func Normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Compute returns a word-level diff between oldText and newText. Both are
// normalized before comparison. Deliberately exact-after-normalization —
// no fuzzy/similarity-threshold matching — see design §4 for why.
func Compute(oldText, newText string) Diff {
	oldWords := strings.Fields(Normalize(oldText))
	newWords := strings.Fields(Normalize(newText))
	ops := diffWords(oldWords, newWords)

	changed := false
	wordsChanged := 0
	wordsUnchanged := 0
	for _, o := range ops {
		switch o.kind {
		case opEqual:
			wordsUnchanged++
		case opDelete, opInsert:
			changed = true
			wordsChanged++
		}
	}

	if !changed {
		return Diff{Changed: false, WordsUnchanged: wordsUnchanged}
	}
	return Diff{
		Changed:        true,
		Render:         renderOps(ops),
		WordsChanged:   wordsChanged,
		WordsUnchanged: wordsUnchanged,
	}
}

// diffWords computes a word-level LCS-based diff, returning the edit
// script as a sequence of equal/delete/insert operations. O(n*m) time and
// space, which is fine for typical skill-response lengths (hundreds of
// words); not intended for huge documents.
func diffWords(a, b []string) []op {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{opEqual, a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{opDelete, a[i]})
			i++
		default:
			ops = append(ops, op{opInsert, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{opDelete, a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{opInsert, b[j]})
	}
	return ops
}

const contextWords = 3

// renderOps produces a compact unified-style rendering: runs of unchanged
// words longer than 2*contextWords collapse to "..." with a few words of
// context on each side of a change, so the output stays scannable instead
// of dumping the whole response on every diff.
func renderOps(ops []op) string {
	var b strings.Builder
	for i := 0; i < len(ops); {
		if ops[i].kind == opEqual {
			j := i
			for j < len(ops) && ops[j].kind == opEqual {
				j++
			}
			run := ops[i:j]
			atStart := i == 0
			atEnd := j == len(ops)
			switch {
			case len(run) > 2*contextWords && !atStart && !atEnd:
				writeWords(&b, run[:contextWords])
				b.WriteString(" ... ")
				writeWords(&b, run[len(run)-contextWords:])
			case atStart && len(run) > contextWords:
				b.WriteString("... ")
				writeWords(&b, run[len(run)-contextWords:])
			case atEnd && len(run) > contextWords:
				writeWords(&b, run[:contextWords])
				b.WriteString(" ...")
			default:
				writeWords(&b, run)
			}
			b.WriteString(" ")
			i = j
			continue
		}

		var deleted, inserted []string
		j := i
		for j < len(ops) && ops[j].kind != opEqual {
			if ops[j].kind == opDelete {
				deleted = append(deleted, ops[j].word)
			} else {
				inserted = append(inserted, ops[j].word)
			}
			j++
		}
		if len(deleted) > 0 {
			fmt.Fprintf(&b, "-[%s]", strings.Join(deleted, " "))
		}
		if len(inserted) > 0 {
			fmt.Fprintf(&b, "+[%s]", strings.Join(inserted, " "))
		}
		b.WriteString(" ")
		i = j
	}
	return strings.TrimSpace(b.String())
}

func writeWords(b *strings.Builder, ops []op) {
	var words []string
	for _, o := range ops {
		words = append(words, o.word)
	}
	b.WriteString(strings.Join(words, " "))
}
