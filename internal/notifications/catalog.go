package notifications

import (
	"cmp"
	"slices"
	"strings"
)

// CatalogEntry is one row in GET .../notification-codes.
type CatalogEntry struct {
	Code        int16  `json:"code"`
	Name        string `json:"name"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
}

// CatalogResponse is the JSON body for GET .../notification-codes.
type CatalogResponse struct {
	Meta struct {
		Count int `json:"count"`
	} `json:"meta"`
	Data []CatalogEntry `json:"data"`
}

// pluginCatalogCodes lists codes relevant to each recommendation plugin (includes reserved codes).
var pluginCatalogCodes = map[string][]int16{
	"container": {1, 2, 3, 5, 6, 7, 8, 9, 21, 22, 25},
	"namespace": {1, 2, 7, 9},
	"node":      {4, 11, 12, 13, 14, 15, 16, 17, 23, 24, 25, 36, 74, 75, 76},
	"gpu":       {10, 26, 27, 28, 36},
	"pvc":       {20, 25, 29, 30},
	"snapshot":  {31, 32, 33, 34, 35},
	"vm": {
		18, 19, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49,
		50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69,
	},
	"quota":         {70, 71, 72},
	"cluster-quota": {70, 71, 72, 73},
}

// BuildCatalog returns notification catalog entries sorted by code ascending.
// pluginFilter, when non-empty, limits results to codes associated with that plugin name.
func BuildCatalog(pluginFilter string) CatalogResponse {
	pluginFilter = strings.TrimSpace(strings.ToLower(pluginFilter))

	var allowed map[int16]struct{}
	if pluginFilter != "" {
		codes, ok := pluginCatalogCodes[pluginFilter]
		if !ok {
			return CatalogResponse{}
		}
		allowed = make(map[int16]struct{}, len(codes))
		for _, code := range codes {
			allowed[code] = struct{}{}
		}
	}

	entries := make([]CatalogEntry, 0, len(Definitions))
	for code, def := range Definitions {
		if allowed != nil {
			if _, ok := allowed[code]; !ok {
				continue
			}
		}
		name := CodeNames[code]
		entries = append(entries, CatalogEntry{
			Code:        code,
			Name:        name,
			Severity:    def.Severity,
			Description: def.Message,
		})
	}

	slices.SortFunc(entries, func(a, b CatalogEntry) int {
		return cmp.Compare(a.Code, b.Code)
	})

	var resp CatalogResponse
	resp.Meta.Count = len(entries)
	resp.Data = entries
	return resp
}
