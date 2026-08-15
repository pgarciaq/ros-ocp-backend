package engine

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// Canonical types live in types/. Aliases here so the runner API is one import.
type DigestRow = types.DigestRow
type KeyedDigest = types.KeyedDigest
type EngineConfig = types.EngineConfig
type EmitContainer = types.EmitContainer
type ContainerRec = types.ContainerRec
type ContainerKey = types.ContainerKey
type TermConfig = types.TermConfig
type CPUConfig = types.CPUConfig
type MemoryConfig = types.MemoryConfig
type SizingThresholdSettings = types.SizingThresholdSettings
