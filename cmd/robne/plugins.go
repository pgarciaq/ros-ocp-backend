package main

import (
	"fmt"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

func dropPlugin(plugins []string, name string) []string {
	out := make([]string, 0, len(plugins))
	for _, p := range plugins {
		if p != name {
			out = append(out, p)
		}
	}
	return out
}

func loadedHasKind(loaded csv.LoadResult, kind csv.Kind) bool {
	for _, name := range loaded.Files {
		if csv.ClassifyFilename(name) == kind {
			return true
		}
	}
	return false
}

func rowsHaveGPU(rows []csv.Row) bool {
	for _, r := range rows {
		if r.HasGPU() {
			return true
		}
	}
	return false
}

func applyFilePlugins(plugins []string, explicit bool, loaded csv.LoadResult) ([]string, error) {
	if !explicit {
		return pruneMissingFilePlugins(plugins, loaded), nil
	}
	if err := requireExplicitFilePlugins(plugins, loaded); err != nil {
		return nil, err
	}
	return plugins, nil
}

func pruneMissingFilePlugins(plugins []string, loaded csv.LoadResult) []string {
	out := make([]string, 0, len(plugins))
	for _, p := range plugins {
		switch p {
		case "container":
			if len(loaded.Rows) > 0 {
				out = append(out, p)
			}
		case "node":
			if len(loaded.Rows) > 0 {
				out = append(out, p)
			}
		case "gpu":
			if rowsHaveGPU(loaded.Rows) {
				out = append(out, p)
			}
		case "namespace":
			if len(loaded.NamespaceRows) > 0 {
				out = append(out, p)
			}
		case "quota":
			if len(loaded.NamespaceRows) > 0 {
				out = append(out, p)
			}
		case "pvc":
			if len(loaded.PVCRows) > 0 {
				out = append(out, p)
			}
		case "vm":
			if len(loaded.VMRows) > 0 {
				out = append(out, p)
			}
		case "cluster_quota":
			if len(loaded.ClusterQuotaRows) > 0 {
				out = append(out, p)
			}
		case "snapshot":
			if loadedHasKind(loaded, csv.KindSnapshot) {
				out = append(out, p)
			}
		default:
			out = append(out, p)
		}
	}
	return out
}

func requireExplicitFilePlugins(plugins []string, loaded csv.LoadResult) error {
	wantC := pluginEnabled(plugins, "container")
	wantNS := pluginEnabled(plugins, "namespace")
	wantNode := pluginEnabled(plugins, "node")
	wantGPU := pluginEnabled(plugins, "gpu")
	wantPVC := pluginEnabled(plugins, "pvc")
	wantVM := pluginEnabled(plugins, "vm")
	wantQuota := pluginEnabled(plugins, "quota")
	wantCRQ := pluginEnabled(plugins, "cluster_quota")
	wantSnap := pluginEnabled(plugins, "snapshot")
	needContainerRows := wantC || wantNode || wantGPU
	if needContainerRows && len(loaded.Rows) == 0 {
		if wantNode || wantGPU {
			return fmt.Errorf("no ROS container CSV found (node and gpu plugins read container ROS rows)")
		}
		return fmt.Errorf("no ROS container CSV found (namespace files require --plugins namespace)")
	}
	if (wantNS || wantQuota) && len(loaded.NamespaceRows) == 0 {
		return fmt.Errorf("no ROS namespace CSV found")
	}
	if wantCRQ && len(loaded.ClusterQuotaRows) == 0 {
		return fmt.Errorf("no cluster-quota CSV found (cluster_quota plugin reads ocp_ros_cluster_quota / ros-openshift-cluster-quota)")
	}
	if wantPVC && len(loaded.PVCRows) == 0 {
		return fmt.Errorf("no storage CSV found (pvc plugin reads ocp_storage_usage / ros-openshift-storage)")
	}
	if wantVM && len(loaded.VMRows) == 0 {
		return fmt.Errorf("no VM usage CSV found (vm plugin reads ocp_ros_vm_usage / ros-openshift-vm-usage)")
	}
	if wantSnap && !loadedHasKind(loaded, csv.KindSnapshot) {
		return fmt.Errorf("no snapshot inventory CSV found (snapshot plugin reads ocp_snapshot_inventory / ros-openshift-snapshot)")
	}
	return nil
}

func pruneEmptyPathAPlugins(fl *fileLoad) {
	if fl.pluginsExplicit {
		return
	}
	out := make([]string, 0, len(fl.plugins))
	for _, p := range fl.plugins {
		switch p {
		case "container":
			if len(fl.containerDigests) > 0 {
				out = append(out, p)
			}
		case "namespace":
			if len(fl.namespaceGrouped) > 0 {
				out = append(out, p)
			}
		case "node":
			if len(fl.nodeDigests) > 0 {
				out = append(out, p)
			}
		case "gpu":
			if len(fl.gpuGrouped) > 0 {
				out = append(out, p)
			}
		case "pvc":
			if len(fl.pvcGrouped) > 0 {
				out = append(out, p)
			}
		case "vm":
			if len(fl.vmDigests) > 0 {
				out = append(out, p)
			}
		case "quota":
			if len(fl.quotaDaily) > 0 {
				out = append(out, p)
			}
		case "cluster_quota":
			if len(fl.clusterQuotaDaily) > 0 {
				out = append(out, p)
			}
		case "snapshot":
			// Path A has no snapshot digest table.
		default:
			out = append(out, p)
		}
	}
	fl.plugins = out
}
