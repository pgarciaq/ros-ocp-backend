package services

import (
	"reflect"

	"github.com/redhatinsights/ros-ocp-backend/internal/utils/settings"
)

// HasSettingsChanged reports whether incoming settings differ from stored.
// Nil stored is always treated as a change (first message / cold start).
func HasSettingsChanged(stored, incoming *settings.CustomTimeframesResponse) bool {
	if stored == nil || incoming == nil {
		return true
	}
	return !reflect.DeepEqual(*stored, *incoming)
}
