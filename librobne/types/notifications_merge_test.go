package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergeNotificationCodes_KeepsHighCodes(t *testing.T) {
	assert.Equal(t, []int16{64}, MergeNotificationCodes(nil, 64))
	assert.Equal(t, []int16{70}, MergeNotificationCodes(nil, 70))
	assert.Equal(t, []int16{79}, MergeNotificationCodes(nil, 79))
}

func TestMergeNotificationCodes_MixLowAndHigh(t *testing.T) {
	got := MergeNotificationCodes([]int16{25, 70}, 1, 79)
	assert.Equal(t, []int16{1, 25, 70, 79}, got)
}

func TestMergeNotificationCodes_DuplicatesStayUnique(t *testing.T) {
	got := MergeNotificationCodes([]int16{70, 79, 70}, 79, 64)
	assert.Equal(t, []int16{64, 70, 79}, got)
}

func TestMergeNotificationCodes_SkipsBelowOne(t *testing.T) {
	got := MergeNotificationCodes([]int16{0, -1, 70}, 0, 79)
	assert.Equal(t, []int16{70, 79}, got)
}

func TestMergeNotificationCodes_Empty(t *testing.T) {
	assert.Nil(t, MergeNotificationCodes(nil))
	assert.Nil(t, MergeNotificationCodes([]int16{}))
	assert.Nil(t, MergeNotificationCodes([]int16{0, -3}))
}

func TestAppendUnique_KeepsHighCodes(t *testing.T) {
	got := AppendUnique(nil, 77)
	got = AppendUnique(got, 79)
	got = AppendUnique(got, 77)
	assert.Equal(t, []int16{77, 79}, got)
}
