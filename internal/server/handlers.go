package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

func healthHandler(svc service.BoardService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()

		columns, err := svc.ListColumns(ctx)
		if err != nil {
			http.Error(w, "failed to list columns", http.StatusInternalServerError)
			return
		}

		var colResponses []ColumnResponse
		for _, col := range columns {
			cards, err := svc.ListCards(ctx, col.Name)
			if err != nil {
				http.Error(w, "failed to list cards", http.StatusInternalServerError)
				return
			}

			cardResponses := make([]CardResponse, len(cards))
			for i, c := range cards {
				cardResponses[i] = CardResponse{
					Key:     c.ID,
					Summary: c.Summary,
					Status:  c.Status,
				}
			}

			colResponses = append(colResponses, ColumnResponse{
				Name:  col.Name,
				Cards: cardResponses,
			})
		}

		resp := HealthResponse{
			Status:  "ok",
			Columns: colResponses,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	}
}
