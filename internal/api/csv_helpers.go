package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

func int16SliceStr(codes []int16) string {
	if len(codes) == 0 {
		return ""
	}
	parts := make([]string, len(codes))
	for i, c := range codes {
		parts[i] = strconv.FormatInt(int64(c), 10)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func notificationMapCodesStr(m map[string]notifications.NotificationEntry) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return "[" + strings.Join(keys, ",") + "]"
}

func nodeContainerRefsStr(refs []model.NodeContainerRef) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = fmt.Sprintf("%s/%s/%s", r.Namespace, r.Workload, r.Container)
	}
	return strings.Join(parts, ";")
}

func optionalInt64CSV(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

func optionalFloat64CSV(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func optionalIntCSV(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func optionalInt32CSV(v *int32) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(int64(*v), 10)
}

func optionalFloat32CSV(v *float32) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(float64(*v), 'f', -1, 32)
}

func vmNotificationsCSV(notifs []any) string {
	if len(notifs) == 0 {
		return ""
	}
	b, err := json.Marshal(notifs)
	if err != nil {
		return ""
	}
	return string(b)
}
