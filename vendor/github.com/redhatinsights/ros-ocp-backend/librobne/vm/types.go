package vm

// Digest is the in-memory VM daily digest used by compute.
// Same underlying type as DailyVMDigest. Do not add a converter or a second struct.
type Digest = DailyVMDigest

// Recommendation is the in-memory VM recommendation used by compute.
// Same underlying type as VMRecommendation.
type Recommendation = VMRecommendation
