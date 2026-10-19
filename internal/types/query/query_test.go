package query

import "testing"

func TestConstructorsAcceptOnlySearchableFields(t *testing.T) {
	tests := []struct {
		name    string
		build   func() error
		wantErr bool
	}{
		{name: "match on a searchable field", build: func() error {
			_, err := NewMatch(FieldDescription, "smith")
			return err
		}},
		{name: "match on an unknown field", wantErr: true, build: func() error {
			_, err := NewMatch("body", "climate")
			return err
		}},
		{name: "match without text", wantErr: true, build: func() error {
			_, err := NewMatch(FieldTitle, "")
			return err
		}},
		{name: "phrase on searchable fields", build: func() error {
			_, err := NewPhrase("climate change", []Field{FieldTitle, FieldContent})
			return err
		}},
		{name: "phrase with one unknown field", wantErr: true, build: func() error {
			_, err := NewPhrase("climate change", []Field{FieldTitle, "headline"})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.build()
			if tt.wantErr != (err != nil) {
				t.Errorf("err = %v, want error: %v", err, tt.wantErr)
			}
		})
	}
}
