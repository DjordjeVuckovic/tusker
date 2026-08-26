package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DjordjeVuckovic/tusker/internal/storage"
	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainIndexer models Postgres: vectors live elsewhere.
type plainIndexer struct{}

func (plainIndexer) Save(context.Context, document.Article) (uuid.UUID, error) {
	return uuid.New(), nil
}
func (plainIndexer) SaveBulk(context.Context, []document.Article) error { return nil }

// colocatedIndexer models Elasticsearch: vectors live on the article document.
type colocatedIndexer struct {
	plainIndexer
	embedded int64
	err      error
}

func (c colocatedIndexer) CountEmbedded(context.Context) (int64, error) {
	return c.embedded, c.err
}

func TestGuardStoredEmbeddings(t *testing.T) {
	allow := func(int64) (bool, error) { return true, nil }
	deny := func(int64) (bool, error) { return false, nil }

	tests := []struct {
		name    string
		storer  storage.Indexer
		confirm confirmReset
		wantErr string
	}{
		{
			name:    "backend that stores vectors separately is never asked",
			storer:  plainIndexer{},
			confirm: deny,
		},
		{
			name:    "nothing embedded yet, so nothing to confirm",
			storer:  colocatedIndexer{embedded: 0},
			confirm: deny,
		},
		{
			name:    "declining leaves the corpus alone",
			storer:  colocatedIndexer{embedded: 707910},
			confirm: deny,
			wantErr: "would drop stored embeddings",
		},
		{
			name:    "accepting proceeds",
			storer:  colocatedIndexer{embedded: 707910},
			confirm: allow,
		},
		{
			name:    "a failed count blocks the load rather than assuming zero",
			storer:  colocatedIndexer{err: errors.New("index unreachable")},
			confirm: allow,
			wantErr: "count stored embeddings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := guardStoredEmbeddings(context.Background(), tt.storer, tt.confirm)

			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestResetConfirmer_Answers(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    bool
		prompts bool
	}{
		{name: "y accepts", input: "y\n", want: true, prompts: true},
		{name: "yes accepts", input: "YES\n", want: true, prompts: true},
		{name: "padding and case are ignored", input: "  Y  \n", want: true, prompts: true},
		{name: "n declines", input: "n\n", prompts: true},
		{name: "anything else declines", input: "maybe\n", prompts: true},
		{name: "a bare newline declines", input: "\n", prompts: true},
		{name: "an answer without a trailing newline still counts", input: "y", want: true, prompts: true},
		{name: "ctrl-D on an empty line declines", input: "", prompts: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			confirm := resetConfirmer(promptOptions{
				Interactive: true,
				In:          strings.NewReader(tt.input),
				Out:         &out,
			})

			got, err := confirm(42)

			require.NoError(t, err, "a declined prompt is an answer, not a failure")
			assert.Equal(t, tt.want, got)
			assert.Contains(t, out.String(), "42", "the operator must see what is at stake")
		})
	}
}

func TestResetConfirmer_ProceedsWithoutAsking(t *testing.T) {
	tests := []struct {
		name string
		opts promptOptions
	}{
		{
			name: "a scripted run must not block on a stdin nobody is watching",
			opts: promptOptions{Interactive: false, In: strings.NewReader("")},
		},
		{
			name: "the env override skips the question",
			opts: promptOptions{AllowAlways: true, Interactive: true, In: strings.NewReader("n\n")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			tt.opts.Out = &out

			got, err := resetConfirmer(tt.opts)(42)

			require.NoError(t, err)
			assert.True(t, got)
			assert.Empty(t, out.String(), "nothing should be printed when nothing is asked")
		})
	}
}
