package research

import (
	"mime"
	"net/http"
	"path"
	"strings"
)

func (s *Server) documentFigure(w http.ResponseWriter, r *http.Request) {
	d, err := s.Store.ResearchDocument(r.PathValue("documentID"))
	if err != nil {
		handleError(w, err)
		return
	}
	manifest, err := s.loadFigureManifest(r.Context(), d)
	if err != nil {
		handleError(w, err)
		return
	}
	name := r.PathValue("name")
	allowed := false
	for _, figure := range manifest.Figures {
		if name == figure.Name || name == figure.PDF {
			allowed = true
		}
	}
	if !allowed {
		apiError(w, 404, "figure_not_found", "Figure is not available")
		return
	}
	for _, asset := range manifest.Assets {
		if asset.Name == name {
			raw, err := s.readDocumentAsset(r.Context(), d, asset)
			if err != nil {
				handleError(w, err)
				return
			}
			kind := "image/png"
			if strings.HasSuffix(name, ".pdf") {
				kind = "application/pdf"
			}
			w.Header().Set("Content-Type", kind)
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": path.Base(name)}))
			_, _ = w.Write(raw)
			return
		}
	}
	apiError(w, 404, "figure_not_found", "Figure is not available")
}
