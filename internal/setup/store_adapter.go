package setup

import (
	"context"

	"github.com/cpave3/legato/internal/engine/store"
)

// StoreAdapter adapts store.Store to the ColumnSeeder interface.
type StoreAdapter struct {
	S *store.Store
}

func (a *StoreAdapter) ListColumnMappings(ctx context.Context) ([]ColumnMappingRow, error) {
	mappings, err := a.S.ListColumnMappings(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]ColumnMappingRow, len(mappings))
	for i, m := range mappings {
		rows[i] = ColumnMappingRow{
			ID:               m.ID,
			ColumnName:       m.ColumnName,
			RemoteStatuses:   m.RemoteStatuses,
			RemoteTransition: m.RemoteTransition,
			SortOrder:        m.SortOrder,
		}
	}
	return rows, nil
}

func (a *StoreAdapter) CreateColumnMapping(ctx context.Context, m ColumnMappingRow) error {
	return a.S.CreateColumnMapping(ctx, store.ColumnMapping{
		ColumnName:       m.ColumnName,
		RemoteStatuses:   m.RemoteStatuses,
		RemoteTransition: m.RemoteTransition,
		SortOrder:        m.SortOrder,
	})
}

func (a *StoreAdapter) DeleteColumnMapping(ctx context.Context, id int) error {
	return a.S.DeleteColumnMapping(ctx, id)
}
