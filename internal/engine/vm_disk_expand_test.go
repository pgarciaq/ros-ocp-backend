package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiskExpand_Uses30DayWindow(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.DiskProjectionWindowDays = 30
	expand := vmComputeDiskExpandGiB(100, 1.0, cfg)
	// target = (100 + 1*30) * 1.25 = 162.5 → ceil to step 10 = 170
	assert.Equal(t, int32(170), expand)
}

func TestDiskExpand_CustomWindow(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.DiskProjectionWindowDays = 60
	expand := vmComputeDiskExpandGiB(50, 0.5, cfg)
	// target = (50 + 0.5*60) * 1.25 = 100 → step 10
	assert.Equal(t, int32(100), expand)
}

func TestDiskExpand_180DayWindow(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.DiskProjectionWindowDays = 180
	expand30 := vmComputeDiskExpandGiB(10, 1.0, DefaultVMRecConfig())
	expand180 := vmComputeDiskExpandGiB(10, 1.0, cfg)
	assert.Greater(t, expand180, expand30)
}
