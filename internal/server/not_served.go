package server

import "net/http"

// notServedHandler answers a route under /api/ that Legato does not serve:
// 404, naming the manual's page that lists every route, so an agent that
// guessed a route finds the right one.
func (s *Server) notServedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.writeJSON(w, http.StatusNotFound, map[string]string{
			"error":   "not_found",
			"message": "Legato serves no route " + r.Method + " " + r.URL.Path + ". Every route, with its method, body and class, is on the manual's API page (reference/api).",
		})
	}
}
