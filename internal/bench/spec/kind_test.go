package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const minimalBody = `engines:
  pg:
    type: postgres
    connection: "postgresql://localhost/test"
jobs:
  - name: test
    suite: suite.yaml
    engines: [pg]
`

func TestKind_RequiresEmbedder(t *testing.T) {
	cases := map[Kind]bool{
		KindFTS:        false,
		KindStructured: false,
		KindFuzzy:      false,
		KindSemantic:   true,
		KindHybrid:     true,
		Kind(""):       false,
	}
	for k, want := range cases {
		assert.Equalf(t, want, k.RequiresEmbedder(), "RequiresEmbedder(%q)", k)
	}
}

func TestParse_KindValid(t *testing.T) {
	s, err := Parse([]byte(validSpecHeader + "kind: semantic\n" + minimalBody))
	require.NoError(t, err)
	assert.Equal(t, KindSemantic, s.Kind)
	assert.Empty(t, s.Warnings)
}

func TestParse_KindInvalidRejected(t *testing.T) {
	_, err := Parse([]byte(validSpecHeader + "kind: lexical\n" + minimalBody))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid kind")
}

func TestParse_KindOmittedWarns(t *testing.T) {
	s, err := Parse([]byte(validSpecHeader + minimalBody))
	require.NoError(t, err)
	assert.Equal(t, Kind(""), s.Kind)
	require.Len(t, s.Warnings, 1)
	assert.Contains(t, s.Warnings[0], "no kind")
}
