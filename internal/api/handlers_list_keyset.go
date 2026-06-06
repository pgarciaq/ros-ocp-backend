package api

import (
	"net/http"
)

// listKeysetMeta holds pagination fields shared by SQL list handlers.
type listKeysetMeta struct {
	Count      int
	Limit      int
	Offset     int
	HasNext    bool
	NextCursor string
}

func (m listKeysetMeta) applyOffset(hasCursor bool) int {
	if hasCursor {
		return 0
	}
	return m.Offset
}

func finalizeListLinks(links *Links, req *http.Request, limit int, hasNext bool, nextCursor string) {
	applyKeysetNextLink(links, req, limit, hasNext, nextCursor)
}
