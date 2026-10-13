package api

import (
	"net/http"
)

func finalizeListLinks(links *Links, req *http.Request, limit int, hasNext bool, nextCursor string) {
	applyKeysetNextLink(links, req, limit, hasNext, nextCursor)
}
