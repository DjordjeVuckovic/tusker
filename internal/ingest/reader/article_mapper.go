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
	cfg      *datamapping.DataMapper
	idKind   datamapping.IdStrategyKind
	idSource string
}

func NewArticleMapper(cfg *datamapping.DataMapper) (*ArticleMapper, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	idKind, err := cfg.IdStrategy.ResolvedKind()
	if err != nil {
		return nil, err
	}
	return &ArticleMapper{cfg: cfg, idKind: idKind, idSource: cfg.IdStrategy.Source}, nil
}

// newID produces the id a record starts with. A mapping that also targets ID
// overwrites it, which is how a dataset carrying its own ids keeps them.
func (m *ArticleMapper) newID(record map[string]string) (uuid.UUID, error) {
	if m.idKind != datamapping.IdStrategyUuidV5 {
		return document.NewArticleID(), nil
	}

	name := strings.TrimSpace(record[m.idSource])
	if name == "" {
		return uuid.Nil, &MappingDropError{
			Reason: ReasonEmptyValue,
			Source: m.idSource,
			Target: "ID",
		}
	}
	return document.DeriveArticleID(name), nil
}

func (m *ArticleMapper) Map(record map[string]string) (document.Article, error) {
	id, err := m.newID(record)
	if err != nil {
		return document.Article{}, err
	}

	article := document.Article{ID: id}
	val := reflect.ValueOf(&article).Elem()

	for _, fm := range m.cfg.FieldMappings {
		raw, present := record[fm.Source]
		if !present {
			if fm.Required {
				return document.Article{}, &MappingDropError{
					Reason: ReasonMissingColumn,
					Source: fm.Source,
					Target: fm.Target,
				}
			}
			continue
		}

		if strings.TrimSpace(raw) == "" {
			if fm.Required {
				return document.Article{}, &MappingDropError{
					Reason: ReasonEmptyValue,
					Source: fm.Source,
					Target: fm.Target,
				}
			}
			slog.Debug("skipping empty field", "field", fm.Source)
			continue
		}

		converted, err := convertValueToType(raw, fm.SourceType, m.cfg.DateFormat)
		if err == nil && isBlank(converted) {
			// An unparsable URL normalizes to "" instead of erroring, so a
			// blank result has to fail the required check on its own.
			err = fmt.Errorf("value %q converted to an empty %s", raw, fm.SourceType)
		}
		if err == nil {
			err = m.assign(val, &article, fm.Target, converted)
		}
		if err != nil {
			if fm.Required {
				return document.Article{}, &MappingDropError{
					Reason: ReasonConversion,
					Source: fm.Source,
					Target: fm.Target,
					Err:    err,
				}
			}
			slog.Warn("skipping optional field", "field", fm.Target, "error", err)
		}
	}

	return article, nil
}

// assign routes a converted value to its target. Metadata.Extra.<key> is a
// struct hop then a map key, which reflection cannot walk in one pass, so it is
// matched by prefix ahead of the ordinary struct paths.
func (m *ArticleMapper) assign(val reflect.Value, article *document.Article, target string, converted any) error {
	if key, ok := strings.CutPrefix(target, datamapping.ExtraTargetPrefix); ok {
		if article.Metadata.Extra == nil {
			article.Metadata.Extra = make(map[string]any)
		}
		article.Metadata.Extra[key] = converted
		return nil
	}

	if path := strings.Split(target, "."); len(path) > 1 {
		return AssignNestedField(val, path, converted)
	}
	return AssignField(val, target, converted)
}

func isBlank(converted any) bool {
	switch v := converted.(type) {
	case string:
		return v == ""
	case time.Time:
		return v.IsZero()
	default:
		return false
	}
}

// ArticleDirectMapper reads records that are already canonical
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
		Title:       record["title"],
		Subtitle:    record["subtitle"],
		Content:     record["content"],
		Author:      record["author"],
		Description: record["description"],
		Language:    record["language"],
		URL:         articleURL,
		PublishedAt: publishedAt,
		CreatedAt:   createdAt,
		Metadata: document.ArticleMetadata{
			SourceId:   record["sourceId"],
			SourceName: record["sourceName"],
			Category:   record["category"],
			ImportedAt: importedAt,
		},
		SearchVector: record["search_vector"],
	}, nil
}
