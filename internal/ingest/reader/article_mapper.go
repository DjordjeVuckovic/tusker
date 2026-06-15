package reader

import (
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/DjordjeVuckovic/tusker/pkg/apis/datamapping"
	"github.com/DjordjeVuckovic/tusker/pkg/utils"
	"github.com/google/uuid"
)

type ArticleMapper struct {
	cfg *datamapping.DataMapper
}

func NewArticleMapper(cfg *datamapping.DataMapper) *ArticleMapper {
	return &ArticleMapper{
		cfg: cfg,
	}
}

func (m *ArticleMapper) Map(record map[string]string) (document.Article, error) {
	if err := m.cfg.Validate(); err != nil {
		return document.Article{}, err
	}

	article := document.Article{ID: document.NewArticleID()}
	val := reflect.ValueOf(&article).Elem()

	for _, fm := range m.cfg.FieldMappings {
		sourceVal := record[fm.Source]

		if sourceVal == "" && !fm.Required {
			slog.Debug("skipping empty field", "field", fm.Source)
			continue
		}

		path := strings.Split(fm.Target, ".")

		if len(path) > 1 {
			err := SetNestedField(val, path, sourceVal, fm.SourceType, m.cfg.DateFormat)
			if err != nil {
				if fm.Required {
					slog.Error("failed to set nested field", "field", fm.Target, "error", err)
					return document.Article{}, err
				}
				slog.Warn("skipping optional nested field", "field", fm.Target, "error", err)
				continue
			}

			continue
		}

		err := SetFlatField(val, path[0], sourceVal, fm.SourceType, m.cfg.DateFormat)
		if err != nil {
			if fm.Required {
				slog.Error("failed to set flat field", "field", fm.Target, "error", err)
				return document.Article{}, err
			}
			slog.Warn("skipping optional field", "field", fm.Target, "error", err)
			continue
		}
	}
	return article, nil
}

type ArticleDirectMapper struct{}

func NewArticleDirectMapper() *ArticleDirectMapper {
	return &ArticleDirectMapper{}
}

func (m *ArticleDirectMapper) Map(record map[string]string) (document.Article, error) {
	id, err := uuid.Parse(record["id"])
	if err != nil {
		return document.Article{}, fmt.Errorf("invalid id: %w", err)
	}

	createdAt, err := utils.ParseTimeOptional(record["createdAt"])
	if err != nil {
		createdAt = time.Now()
	}

	title := record["title"]
	subtitle := record["subtitle"]
	content := record["content"]
	author := record["author"]
	description := record["description"]
	language := record["language"]

	articleURL, _ := NormalizeURL(record["url"])

	var publishedAt time.Time
	if t, err := utils.ParseTimeOptional(record["publishedAt"]); err == nil {
		publishedAt = t
	}

	var importedAt time.Time
	if t, err := utils.ParseTimeOptional(record["importedAt"]); err == nil {
		importedAt = t
	}

	return document.Article{
		ID:          id,
		Title:       title,
		Subtitle:    subtitle,
		Content:     content,
		Author:      author,
		Description: description,
		Language:    language,
		CreatedAt:   createdAt,
		URL:         articleURL,
		Metadata: document.ArticleMetadata{
			SourceId:    record["sourceId"],
			SourceName:  record["sourceName"],
			PublishedAt: publishedAt,
			ImportedAt:  importedAt,
			Category:    record["category"],
		},
		SearchVector: record["search_vector"],
	}, nil
}
