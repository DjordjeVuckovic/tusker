package storage

import (
	"context"

	"github.com/DjordjeVuckovic/tusker/internal/types/document"
	"github.com/google/uuid"
)

type Reader interface {
	GetByIDs(ctx context.Context, ids []uuid.UUID) ([]document.Article, error)
}
