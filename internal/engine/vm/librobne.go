package vm

import libvm "github.com/redhatinsights/ros-ocp-backend/librobne/vm"

// Digest is the in-memory VM daily digest used by compute and pgx Scan.
// Same underlying type as model.DailyVMDigest (alias of librobne/vm.DailyVMDigest).
type Digest = libvm.Digest

// Recommendation is the in-memory VM recommendation used by compute and pgx Scan.
type Recommendation = libvm.Recommendation

type GPUDeviceDigest = libvm.GPUDeviceDigest
type PVCDigest = libvm.PVCDigest
type VMRecConfig = libvm.VMRecConfig
type TermWindow = libvm.TermWindow
type InstanceType = libvm.InstanceType
type VMNotification = libvm.VMNotification
type GPUAnalysis = libvm.GPUAnalysis
type VMPreferenceContext = libvm.VMPreferenceContext
type ClusterPreferenceRecord = libvm.ClusterPreferenceRecord
type ClusterContext = libvm.ClusterContext
type VMTimeSliceRecommendation = libvm.VMTimeSliceRecommendation

const (
	VMCategoryAbandoned           = libvm.VMCategoryAbandoned
	VMCategoryPowerOffCandidate   = libvm.VMCategoryPowerOffCandidate
	VMCategoryIdle                = libvm.VMCategoryIdle
	VMCategoryOversized           = libvm.VMCategoryOversized
	VMCategoryUndersized          = libvm.VMCategoryUndersized
	VMCategoryOptimized           = libvm.VMCategoryOptimized
	NotifVMPowerOffSchedule       = libvm.NotifVMPowerOffSchedule
	NotifVMDiskGrowingNoCapacity  = libvm.NotifVMDiskGrowingNoCapacity
	NotifVMNoGuestAgent           = libvm.NotifVMNoGuestAgent
	NotifVMHighIO                 = libvm.NotifVMHighIO
	NotifVMDiskFillingGuest       = libvm.NotifVMDiskFillingGuest
	NotifVMInstanceTypeRec        = libvm.NotifVMInstanceTypeRec
	NotifVMDiskCritical           = libvm.NotifVMDiskCritical
	NotifVMAbandoned              = libvm.NotifVMAbandoned
	NotifVMGuestAgentInterrupted  = libvm.NotifVMGuestAgentInterrupted
	NotifVMInsufficientData       = libvm.NotifVMInsufficientData
	NotifVMUnknownOS              = libvm.NotifVMUnknownOS
	NotifVMWindowsUpdateSpike     = libvm.NotifVMWindowsUpdateSpike
	NotifVMCrashLoop              = libvm.NotifVMCrashLoop
	NotifVMDownsizeHeld           = libvm.NotifVMDownsizeHeld
	NotifVMGPUIdle                = libvm.NotifVMGPUIdle
	NotifVMGPUUnderutilized       = libvm.NotifVMGPUUnderutilized
	NotifVMGPUMemorySaturated     = libvm.NotifVMGPUMemorySaturated
	NotifVMGPUComputeSaturated    = libvm.NotifVMGPUComputeSaturated
	NotifVMGPUMixedIdle           = libvm.NotifVMGPUMixedIdle
	NotifVMVGPUProfileRecommended = libvm.NotifVMVGPUProfileRecommended
	NotifVMGPUTimeSliceUnsafeFB   = libvm.NotifVMGPUTimeSliceUnsafeFB
	NotifVMNetworkSaturated       = libvm.NotifVMNetworkSaturated
	NotifVMIOSequential           = libvm.NotifVMIOSequential
	NotifVMIORandom               = libvm.NotifVMIORandom
	NotifVMRedundantColocation    = libvm.NotifVMRedundantColocation
	NotifVMUnevenNodeDistribution = libvm.NotifVMUnevenNodeDistribution
	NotifVMSharedStorage          = libvm.NotifVMSharedStorage
	NotifVMNUMAOversized          = libvm.NotifVMNUMAOversized
	NotifVMNetworkQoSSRIOV        = libvm.NotifVMNetworkQoSSRIOV
	NotifVMNetworkQoSDPDK         = libvm.NotifVMNetworkQoSDPDK
	NotifVMStorageTierCold        = libvm.NotifVMStorageTierCold
	NotifVMStorageTierIOPS        = libvm.NotifVMStorageTierIOPS
	NotifVMStorageTierThroughput  = libvm.NotifVMStorageTierThroughput
	vmGPUActionRemoveGPU          = libvm.GPUActionRemoveGPU
	vmGPUActionSmallerMIGProfile  = libvm.GPUActionSmallerMIGProfile
	vmGPUActionUseMIGProfile      = libvm.GPUActionUseMIGProfile
)

var (
	DefaultVMRecConfig                 = libvm.DefaultVMRecConfig
	DefaultVMTermWindows               = libvm.DefaultVMTermWindows
	VMTermWindowsFromConfig            = libvm.VMTermWindowsFromConfig
	MaxVMLookbackDays                  = libvm.MaxVMLookbackDays
	DetectVMAbandoned                  = libvm.DetectVMAbandoned
	DetectPowerOffCandidate            = libvm.DetectPowerOffCandidate
	PowerOffIdleRatioBasisPoints       = libvm.PowerOffIdleRatioBasisPoints
	PowerOffIdlePercentFromBasisPoints = libvm.PowerOffIdlePercentFromBasisPoints
	AnalyzeVMGPU                       = libvm.AnalyzeVMGPU
	OptimalMIGProfile                  = libvm.OptimalMIGProfile
	MatchInstanceType                  = libvm.MatchInstanceType
	NormalizeInstanceTypeSeries        = libvm.NormalizeInstanceTypeSeries
	NormalizePreferenceClass           = libvm.NormalizePreferenceClass
	BuildVMPreferenceContext           = libvm.BuildVMPreferenceContext
	buildVMPreferenceContext           = libvm.BuildVMPreferenceContext
	CheckNUMAFit                       = libvm.CheckNUMAFit
	VMExplFromRecommendation           = libvm.VMExplFromRecommendation
	NewClusterContext                  = libvm.NewClusterContext
	DetectSharedPVCs                   = libvm.DetectSharedPVCs
	buildClusterLatestDigests          = libvm.BuildClusterLatestDigests
	buildNodeMemoryGiBMap              = libvm.BuildNodeMemoryGiBMap
)
