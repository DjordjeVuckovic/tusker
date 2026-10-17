package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExport_UnknownFormatListsRun(t *testing.T) {
	err := executeExport(newExportCmd(), exportFlags{format: "bogus"}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "run")
}

func TestExport_HelpDescribesRunFormat(t *testing.T) {
	cmd := newExportCmd()

	assert.Contains(t, cmd.Long, "--format run")
	assert.Contains(t, cmd.Flags().Lookup("format").Usage, "run")
}
