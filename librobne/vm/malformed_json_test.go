package vm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// withMalformedHook captures ReportMalformedJSON sites for the test and
// restores the default no-op reporter afterwards. No t.Parallel: the hook is
// process-wide mutable state.
func withMalformedHook(t *testing.T) *[]string {
	t.Helper()
	var got []string
	types.SetMalformedJSONReporter(func(site string) { got = append(got, site) })
	t.Cleanup(func() { types.SetMalformedJSONReporter(nil) })
	return &got
}

func decodeNotifs(t *testing.T, raw []byte) []VMNotification {
	t.Helper()
	var out []VMNotification
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestAppendVMGPUNotifications_CorruptExistingKeepsNewCodes(t *testing.T) {
	got := withMalformedHook(t)
	out := appendVMGPUNotifications([]byte(`{"oops":`), []int16{NotifVMGPUIdle})
	notifs := decodeNotifs(t, out)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMGPUIdle, notifs[0].Code)
	assert.Equal(t, []string{types.SiteVMGPUNotifications}, *got)
}

func TestAppendVMPlacementNotifications_CorruptExistingKeepsNewCodes(t *testing.T) {
	got := withMalformedHook(t)
	out := appendVMPlacementNotifications(
		[]byte(`[{"code":`),
		[]VMNotification{{Code: NotifVMRedundantColocation, Type: vmNotifTypeInfo, Message: "x"}},
	)
	notifs := decodeNotifs(t, out)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMRedundantColocation, notifs[0].Code)
	assert.Equal(t, []string{types.SiteVMPlacementNotifications}, *got)
}

func TestAppendVMNotifications_ValidExistingPreserved(t *testing.T) {
	got := withMalformedHook(t)
	existing, err := json.Marshal([]VMNotification{{Code: NotifVMGPUIdle, Type: vmNotifTypeWarning, Message: "idle"}})
	require.NoError(t, err)
	out := appendVMGPUNotifications(existing, []int16{NotifVMGPUUnderutilized})
	notifs := decodeNotifs(t, out)
	require.Len(t, notifs, 2)
	assert.Equal(t, NotifVMGPUIdle, notifs[0].Code)
	assert.Equal(t, NotifVMGPUUnderutilized, notifs[1].Code)
	assert.Empty(t, *got, "valid existing JSON must not report")
}
