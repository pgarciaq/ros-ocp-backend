package vm

import "encoding/json"

func appendVMPlacementNotifications(existing []byte, extra []VMNotification) []byte {
	if len(extra) == 0 {
		return existing
	}
	var notifs []VMNotification
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &notifs)
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
