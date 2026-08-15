package pvc

import libpvc "github.com/redhatinsights/ros-ocp-backend/librobne/pvc"

const (
	PVCRecTypeOversized = libpvc.PVCRecTypeOversized
	PVCRecTypeNearFull  = libpvc.PVCRecTypeNearFull
	PVCRecTypeOrphaned  = libpvc.PVCRecTypeOrphaned
	PVCRecTypeHealthy   = libpvc.PVCRecTypeHealthy
)

type PVCKey = libpvc.PVCKey
type EngineConfig = libpvc.EngineConfig
type PVCDigestRow = libpvc.PVCDigestRow
type PVCRec = libpvc.PVCRec
type ThresholdSettings = libpvc.ThresholdSettings

var (
	DefaultThresholdSettings = libpvc.DefaultThresholdSettings
	PVCConfidenceLevel       = libpvc.PVCConfidenceLevel
	EvaluatePVCNotifications = libpvc.EvaluatePVCNotifications
	ComputePVCRecommendation = libpvc.ComputePVCRecommendation
)
