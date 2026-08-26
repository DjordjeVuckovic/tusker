// Package cli renders operator-facing command output. Diagnostics belong in
// slog; what the operator reads at the end of a run comes through here.
package cli

import (
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/fatih/color"
)

var (
	cOK   = color.New(color.FgGreen, color.Bold)
	cWarn = color.New(color.FgYellow)
	cDim  = color.New(color.FgHiBlack)
)

// Field is one labelled value in a summary block.
type Field struct {
	Label string
	Value string
}

func IntField(label string, value int) Field {
	return Field{Label: label, Value: strconv.Itoa(value)}
}

func Done(w io.Writer, msg string) {
	fmt.Fprintf(w, "%s %s\n", cOK.Sprint("✓"), msg)
}

func Warn(w io.Writer, msg string) {
	fmt.Fprintf(w, "%s %s\n", cWarn.Sprint("⚠"), msg)
}

// Summary prints msg followed by fields aligned on their labels.
func Summary(w io.Writer, msg string, fields ...Field) {
	Done(w, msg)
	width := 0
	for _, f := range fields {
		if n := utf8.RuneCountInString(f.Label); n > width {
			width = n
		}
	}
	for _, f := range fields {
		fmt.Fprintf(w, "  %s  %s\n", cDim.Sprintf("%-*s", width, f.Label), f.Value)
	}
}
