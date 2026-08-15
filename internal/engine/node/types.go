package node

import libnode "github.com/redhatinsights/ros-ocp-backend/librobne/node"

type ThresholdSettings = libnode.ThresholdSettings
type EngineConfig = libnode.EngineConfig
type RecConfig = libnode.RecConfig
type DigestRow = libnode.DigestRow
type Rec = libnode.Rec

var (
	EnginesFromThresholds    = libnode.EnginesFromThresholds
	RecConfigFromThresholds  = libnode.RecConfigFromThresholds
	DefaultThresholdSettings = libnode.DefaultThresholdSettings
)
