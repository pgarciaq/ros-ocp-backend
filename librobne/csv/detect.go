package csv

import (
	"path/filepath"
	"strings"
)

// Kind is the ROS/cost CSV family for a filename (not the file contents).
type Kind int

const (
	KindUnknown Kind = iota
	KindContainerROS
	KindCostOnly
	KindOther
)

// ClassifyFilename maps a path or tar member name to a CSV family.
// Strips a leading "./" before matching (spec §8).
func ClassifyFilename(name string) Kind {
	base := filepath.Base(stripDotSlash(name))
	lower := strings.ToLower(base)

	if strings.HasPrefix(lower, "ros-openshift-container-") {
		return KindContainerROS
	}
	if strings.Contains(lower, "ocp_ros_namespace") {
		return KindOther
	}
	if strings.Contains(lower, "ocp_ros_usage") {
		return KindContainerROS
	}
	if strings.HasPrefix(lower, "cm-openshift-") {
		return KindCostOnly
	}
	if strings.Contains(lower, "ocp_pod_usage") {
		return KindCostOnly
	}
	return KindUnknown
}

func stripDotSlash(name string) string {
	for strings.HasPrefix(name, "./") {
		name = strings.TrimPrefix(name, "./")
	}
	return name
}
