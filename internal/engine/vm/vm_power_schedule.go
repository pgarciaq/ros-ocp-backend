package vm

import (
	"encoding/json"
	"fmt"

	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

const vmNotifTypeInfo = "info"

func vmPowerOffNotificationMessage(idlePct int32, savingsUSD *float64) string {
	msg := fmt.Sprintf(
		"VM is idle %d%% of observed days — consider scheduling power-off during inactive periods.",
		idlePct,
	)
	if savingsUSD != nil && *savingsUSD > 0 {
		msg += fmt.Sprintf(" Estimated monthly savings: $%.2f", *savingsUSD)
	}
	return msg
}

// AppendVMPowerOffNotifications adds or updates notification 64 on recommendations that are
// power-off candidates. Call after savings are computed so the message can include estimates.
func AppendVMPowerOffNotifications(recs []Recommendation) {
	for i := range recs {
		if recs[i].Category != VMCategoryPowerOffCandidate {
			continue
		}
		pct := int32(0)
		if recs[i].PowerOffIdleRatio != nil {
			pct = PowerOffIdlePercentFromBasisPoints(*recs[i].PowerOffIdleRatio)
		}
		var savings *float64
		if recs[i].EstimatedSavingsCents != nil {
			usd := money.CentsToUSD(*recs[i].EstimatedSavingsCents)
			savings = &usd
		}
		recs[i].Notifications = appendVMPowerOffNotificationJSON(recs[i].Notifications, pct, savings)
	}
}

func appendVMPowerOffNotificationJSON(raw []byte, idlePct int32, savingsUSD *float64) []byte {
	var out []VMNotification
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	filtered := out[:0]
	for _, n := range out {
		if n.Code != NotifVMPowerOffSchedule {
			filtered = append(filtered, n)
		}
	}
	filtered = append(filtered, VMNotification{
		Code:    NotifVMPowerOffSchedule,
		Type:    vmNotifTypeInfo,
		Message: vmPowerOffNotificationMessage(idlePct, savingsUSD),
	})
	b, err := json.Marshal(filtered)
	if err != nil {
		return []byte("[]")
	}
	return b
}
