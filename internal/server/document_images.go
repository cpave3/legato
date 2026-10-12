package server

import (
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/service"
)

// imageOpener is a service that keeps copies of image files and opens one
// by the folder and file an address names.
type imageOpener interface {
	OpenImage(folder, file string) (*service.AgentUIImageFile, error)
}

// documentImageHandler gives out Legato's copies of the image files that
// the Markdown of plans or of reviews shows. Like the images of what agents
// show, they are asked for with no token: an <img> cannot send one. What
// stands in for it is the address, the SHA-256 of the image's content.
func (s *Server) documentImageHandler(route string, opener func() imageOpener) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		folder, file, found := strings.Cut(strings.TrimPrefix(r.URL.Path, route), "/")
		images := opener()
		if !found || images == nil {
			s.writeError(w, http.StatusNotFound, "no such image")
			return
		}
		image, err := images.OpenImage(folder, file)
		if err != nil {
			s.writeError(w, http.StatusNotFound, "no such image")
			return
		}
		defer image.Close()
		serveImageCopy(w, r, image)
	}
}

// serveImageCopy writes one of Legato's kept copies of an image, with the
// headers that keep it an image and nothing more wherever it is opened.
func serveImageCopy(w http.ResponseWriter, r *http.Request, image *service.AgentUIImageFile) {
	header := w.Header()
	header.Set("Content-Type", image.MediaType)
	// The type is the one read from the content, and the browser must not
	// guess another.
	header.Set("X-Content-Type-Options", "nosniff")
	// The address names the content, so what it answers never changes.
	// private: an image may be the person's own.
	header.Set("Cache-Control", "private, max-age=31536000, immutable")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, "", image.ModTime, image)
}

// planImages and reviewImages are the plan and review services, when they
// are set and keep image files.
func (s *Server) planImages() imageOpener {
	opener, _ := s.plans.(imageOpener)
	return opener
}

func (s *Server) reviewImages() imageOpener {
	opener, _ := s.reviews.(imageOpener)
	return opener
}
