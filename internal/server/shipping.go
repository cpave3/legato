package server

import (
	"context"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// ShippingSource lists the review loops Ship looks after
// (service.ReviewRunCaller.Shipping).
type ShippingSource interface {
	Shipping(ctx context.Context) (service.ShippingView, error)
}

// SetShipping serves GET /api/shipping, the Shipping page's list.
func (s *Server) SetShipping(source ShippingSource) { s.shipping = source }

// shippingHandler serves GET /api/shipping: {loops: [...]} (LEG-706). A
// server without review passes lists none.
func (s *Server) shippingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.shipping == nil {
			s.writeJSON(w, http.StatusOK, service.ShippingView{Loops: []service.ShippingLoop{}})
			return
		}
		view, err := s.shipping.Shipping(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, view)
	}
}
