package es

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"

	pkgtesting "github.com/DjordjeVuckovic/tusker/pkg/testing"
)

const indexTemplatePath = "../../../configs/elasticsearch/index-template.json"

type indexTemplate struct {
	Template struct {
		Settings struct {
			Analysis map[string]any `json:"analysis"`
		} `json:"settings"`
		Mappings struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"mappings"`
	} `json:"template"`
}

func readIndexTemplate(t *testing.T) indexTemplate {
	t.Helper()
	raw, err := os.ReadFile(indexTemplatePath)
	if err != nil {
		t.Fatalf("read index template: %v", err)
	}
	var tmpl indexTemplate
	if err := json.Unmarshal(raw, &tmpl); err != nil {
		t.Fatalf("parse index template: %v", err)
	}
	return tmpl
}

func TestIndexTemplate_DeclaresTheAnalysisTheLoaderBuilds(t *testing.T) {
	tmpl := readIndexTemplate(t)

	settings := NewIndexBuilder().buildSettings()
	raw, err := json.Marshal(settings.Analysis)
	if err != nil {
		t.Fatalf("marshal loader analysis: %v", err)
	}
	var loaderAnalysis map[string]any
	if err := json.Unmarshal(raw, &loaderAnalysis); err != nil {
		t.Fatalf("unmarshal loader analysis: %v", err)
	}

	if !reflect.DeepEqual(tmpl.Template.Settings.Analysis, loaderAnalysis) {
		got, _ := json.MarshalIndent(tmpl.Template.Settings.Analysis, "", "  ")
		want, _ := json.MarshalIndent(loaderAnalysis, "", "  ")
		t.Errorf("index-template.json analysis drifted from IndexBuilder\ntemplate: %s\nloader:   %s", got, want)
	}
}

func TestIndexTemplate_ReferencesOnlyDeclaredAnalyzers(t *testing.T) {
	tmpl := readIndexTemplate(t)
	declared, _ := tmpl.Template.Settings.Analysis["analyzer"].(map[string]any)

	for field, property := range tmpl.Template.Mappings.Properties {
		for _, key := range []string{"analyzer", "search_analyzer"} {
			name, ok := property[key].(string)
			if !ok {
				continue
			}
			if _, found := declared[name]; !found {
				t.Errorf("field %q uses %s %q, which the template does not declare", field, key, name)
			}
		}
	}
}

func TestIndexer_AnalyzesTextFieldsLikePostgresEnglish(t *testing.T) {
	ctx := context.Background()
	container := pkgtesting.NewESContainer(ctx, t)
	cfg := ClientConfig{Addresses: []string{container.Address}, IndexName: "articles_analyzer_test"}

	if _, err := NewIndexer(ctx, cfg); err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	client, err := newClient(cfg)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	// Expected tokens are the lexemes of to_tsvector('english', text) on
	// PostgreSQL 18, in position order.
	probes := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "snowball stemming after the postgres stopword list",
			text: "about would should all because very after over news skies dying generously fairly",
			want: []string{"would", "news", "sky", "die", "generous", "fair"},
		},
		{
			name: "function words postgres drops entirely",
			text: "the a an and or but if while is was been being have has had do does did",
			want: nil,
		},
	}

	for _, field := range []string{"title", "subtitle", "description", "content"} {
		for _, probe := range probes {
			t.Run(field+"/"+probe.name, func(t *testing.T) {
				res, err := client.Indices.Analyze().Index(cfg.IndexName).Field(field).Text(probe.text).Do(ctx)
				if err != nil {
					t.Fatalf("_analyze: %v", err)
				}
				var got []string
				for _, token := range res.Tokens {
					got = append(got, token.Token)
				}
				if !slices.Equal(got, probe.want) {
					t.Errorf("tokens = %q, want %q", got, probe.want)
				}
			})
		}
	}
}
