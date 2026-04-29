package rbac

import (
	"fmt"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
	"gorm.io/gorm"
)

type ResourceType string

const (
	ResourceContainer ResourceType = "container"
	ResourceProject   ResourceType = "namespace"
	ResourceNode      ResourceType = "node"
)

func AddRBACFilter(query *gorm.DB, userPermissions map[string][]string, resourceType ResourceType) error {
	cfg := config.GetConfig()
	if !cfg.RBACEnabled {
		return nil
	}

	// Validate resource type
	switch resourceType {
	case ResourceContainer, ResourceProject, ResourceNode:
		// valid supported type
	default:
		return fmt.Errorf("unsupported resource type: %s", resourceType)
	}

	// Global wildcard -> full access
	if _, ok := userPermissions["*"]; ok {
		return nil
	}

	clusterPerms, hasCluster := userPermissions["openshift.cluster"]
	projectPerms, hasProject := userPermissions["openshift.project"]
	nodePerms, hasNode := userPermissions["openshift.node"]

	clusterAll := hasCluster && utils.StringInSlice("*", clusterPerms)
	projectAll := hasProject && utils.StringInSlice("*", projectPerms)
	nodeAll := hasNode && utils.StringInSlice("*", nodePerms)

	applyClusterFilter := func() {
		query = query.Where("clusters.cluster_uuid IN (?)", clusterPerms)
	}

	applyProjectFilter := func() {
		switch resourceType {
		case ResourceContainer:
			query = query.Where("workloads.namespace IN (?)", projectPerms)
		case ResourceProject:
			query = query.Where("namespace_recommendation_sets.namespace_name IN (?)", projectPerms)
		}
	}

	applyNodeFilter := func() {
		if resourceType == ResourceNode {
			query = query.Where("gpu_container_digests.node_name IN (?)", nodePerms)
		}
	}

	if resourceType == ResourceNode {
		if hasCluster && !clusterAll {
			applyClusterFilter()
		}
		if hasNode && !nodeAll {
			applyNodeFilter()
		}
		return nil
	}

	// Container / Project path (unchanged logic)
	if hasCluster && hasProject {
		switch {
		case clusterAll && projectAll:
			return nil
		case clusterAll:
			applyProjectFilter()
			return nil
		case projectAll:
			applyClusterFilter()
			return nil
		default:
			applyClusterFilter()
			applyProjectFilter()
			return nil
		}
	}

	if hasCluster && !hasProject {
		if !clusterAll {
			applyClusterFilter()
		}
		return nil
	}

	if hasProject && !hasCluster {
		if !projectAll {
			applyProjectFilter()
		}
		return nil
	}

	return nil
}
