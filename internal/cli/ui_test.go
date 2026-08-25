package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noColor(t *testing.T) {
	t.Helper()
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })
}

func TestSummary_AlignsLabelsAndKeepsValues(t *testing.T) {
	noColor(t)
	var out bytes.Buffer

	Summary(&out, "Embedding load complete",
		IntField("sent", 707910),
		IntField("stored", 707910),
		IntField("dim mismatches", 0),
	)

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	require.Len(t, lines, 4)
	assert.Contains(t, lines[0], "Embedding load complete")
	assert.Contains(t, lines[1], "707910")

	// Every value starts at the same column, so the block reads as a table.
	valueColumn := func(line string) int { return strings.LastIndex(line, "  ") + 2 }
	for _, line := range lines[2:] {
		assert.Equal(t, valueColumn(lines[1]), valueColumn(line), "value column drifted on %q", line)
	}
}

func TestWarn_IsDistinctFromDone(t *testing.T) {
	noColor(t)
	var done, warn bytes.Buffer

	const msg = "3 records skipped"
	Done(&done, msg)
	Warn(&warn, msg)

	assert.NotEqual(t, done.String(), warn.String(),
		"a warning must be distinguishable from success on the same text")
	assert.Contains(t, warn.String(), msg)
}
