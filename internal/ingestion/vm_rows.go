package ingestion

import "time"

// VMRow represents a single row from the ros-openshift-vm-usage CSV.
// Columns marked "guest agent" are nullable (nil when guest agent not installed).
type VMRow struct {
	IntervalStart time.Time
	IntervalEnd   time.Time
	VMName        string
	Namespace     string
	NodeName      string
	GuestOS       string // from kubevirt_vmi_info os label; may be empty

	// CPU (millicores)
	CPUUsageMC   float64
	CPURequestMC float64
	CPULimitMC   float64

	// Memory (KiB)
	MemoryUsageKiB     float64
	MemoryRequestKiB   float64
	MemoryAvailableKiB *float64 // guest agent — nil if not present

	// Disk
	DiskAllocatedBytes float64

	// Filesystem (guest agent)
	FilesystemUsedBytes     *float64 // guest agent
	FilesystemCapacityBytes *float64 // guest agent

	// I/O (virt-handler level, always available)
	DiskReadIOPS         *float64
	DiskWriteIOPS        *float64
	DiskReadBytesPerSec  *float64
	DiskWriteBytesPerSec *float64

	// RestartCount is the number of VMI transitions to Running in the interval (nil if unavailable).
	RestartCount *int32
}
