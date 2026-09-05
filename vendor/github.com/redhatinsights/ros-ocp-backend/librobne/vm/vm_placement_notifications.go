package vm

import (
	"encoding/json"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

func appendVMPlacementNotifications(existing []byte, extra []VMNotification) []byte {
	if len(extra) == 0 {
		return existing
	}
	var notifs []VMNotification
	if len(existing) > 0 {
		// Keep-going on corrupt stored JSON (#538): decode into a temp
		// and use empty on error, so a partial decode can never leak
		// through. New codes below still append.
		var decoded []VMNotification
		if err := json.Unmarshal(existing, &decoded); err != nil {
			types.ReportMalformedJSON(types.SiteVMPlacementNotifications)
		} else {
			notifs = decoded
		}
	}
	notifs = append(notifs, extra...)
	b, err := json.Marshal(notifs)
	if err != nil {
		return existing
	}
	return b
}

func vmPlacementFlagsFromNotifications(notifs []VMNotification) (redundant, sharedStorage, numaOversized bool) {
	for _, n := range notifs {
		switch n.Code {
		case NotifVMRedundantColocation:
			redundant = true
		case NotifVMSharedStorage:
			sharedStorage = true
		case NotifVMNUMAOversized:
			numaOversized = true
		}
	}
	return redundant, sharedStorage, numaOversized
}
