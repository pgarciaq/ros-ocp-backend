package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

type rateCardFile struct {
	Currency      string                  `json:"currency"`
	MarkupPercent *float64                `json:"markup_percent"`
	Clusters      map[string]clusterRates `json:"clusters"`
}

type clusterRates struct {
	Distribution  string       `json:"distribution"`
	MarkupPercent *float64     `json:"markup_percent"`
	CPU           cpuRates     `json:"cpu"`
	Memory        memoryRates  `json:"memory"`
	GPU           gpuRates     `json:"gpu"`
	Storage       storageRates `json:"storage"`
}

type cpuRates struct {
	DefaultDollarsPerCoreHour float64            `json:"default_dollars_per_core_hour"`
	ByArchitecture            map[string]float64 `json:"by_architecture"`
	ByInstanceType            map[string]float64 `json:"by_instance_type"`
}

type memoryRates struct {
	DefaultDollarsPerGiBHour float64 `json:"default_dollars_per_gib_hour"`
}

type gpuRates struct {
	DefaultDollarsPerGPUMonth float64            `json:"default_dollars_per_gpu_month"`
	DollarsPerGPUHour         float64            `json:"dollars_per_gpu_hour"`
	ByModel                   map[string]float64 `json:"by_model"`
}

type storageRates struct {
	DefaultDollarsPerGiBMonth float64 `json:"default_dollars_per_gib_month"`
}

func loadRateCardFile(env overlayEnv, rateCardFlag string) (*rateCardFile, error) {
	merged := &rateCardFile{Clusters: map[string]clusterRates{}}
	loaded := false
	if user := firstExistingUserFile(env, "rate-card.json", ".rate-card.json"); user != "" {
		if err := mergeRateCardFile(merged, user); err != nil {
			return nil, err
		}
		loaded = true
	}
	if rateCardFlag != "" {
		if err := mergeRateCardFile(merged, rateCardFlag); err != nil {
			return nil, err
		}
		loaded = true
	} else {
		cwd := filepath.Join(env.Cwd, "rate-card.json")
		if fileExists(cwd) {
			if err := mergeRateCardFile(merged, cwd); err != nil {
				return nil, err
			}
			loaded = true
		}
	}
	if !loaded {
		return nil, nil
	}
	if len(merged.Clusters) == 0 {
		return nil, fmt.Errorf("rate card has no clusters map entries")
	}
	return merged, nil
}

func mergeRateCardFile(dst *rateCardFile, path string) error {
	raw, err := os.ReadFile(filepath.Clean(path)) //nolint:gosec // G304: explicit CLI/config overlay path
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var src rateCardFile
	if err := json.Unmarshal(raw, &src); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if src.Currency != "" {
		dst.Currency = src.Currency
	}
	if src.MarkupPercent != nil {
		v := *src.MarkupPercent
		dst.MarkupPercent = &v
	}
	if dst.Clusters == nil {
		dst.Clusters = map[string]clusterRates{}
	}
	for id, cluster := range src.Clusters {
		dst.Clusters[id] = cluster
	}
	return nil
}

func resolveClusterID(cfg fileConfig, rows []csv.Row) (string, error) {
	return resolveClusterIDs(cfg, csv.UniqueClusterIDs(rows))
}

func resolveClusterIDFromLoad(cfg fileConfig, loaded csv.LoadResult) (string, error) {
	ids := append([]string{}, csv.UniqueClusterIDs(loaded.Rows)...)
	ids = append(ids, csv.UniqueNamespaceClusterIDs(loaded.NamespaceRows)...)
	seen := map[string]struct{}{}
	var uniq []string
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	return resolveClusterIDs(cfg, uniq)
}

func resolveClusterIDs(cfg fileConfig, ids []string) (string, error) {
	if len(ids) > 1 {
		return "", fmt.Errorf("phase 1 supports one cluster per input; found %v", ids)
	}
	csvID := ""
	if len(ids) == 1 {
		csvID = ids[0]
	}
	yamlID := cfg.ClusterUUID
	if yamlID != "" && csvID != "" && yamlID != csvID {
		return "", fmt.Errorf("cluster_uuid %q disagrees with CSV cluster_id %q", yamlID, csvID)
	}
	if yamlID != "" {
		return yamlID, nil
	}
	return csvID, nil
}

func rateCardForRow(file *rateCardFile, clusterID string, meta csv.RowMeta, hoursPerMonth int64) (*types.RateCard, error) {
	if file == nil {
		return nil, nil
	}
	if clusterID == "" {
		return nil, fmt.Errorf("cluster_uuid is required when a rate card is present")
	}
	cluster, ok := file.Clusters[clusterID]
	if !ok {
		return nil, fmt.Errorf("rate card has no cluster %q", clusterID)
	}
	markup := 0.0
	if file.MarkupPercent != nil {
		markup = *file.MarkupPercent
	}
	if cluster.MarkupPercent != nil {
		markup = *cluster.MarkupPercent
	}
	cpuDollars := lookupCPU(cluster.CPU, meta)
	gpuMonth := lookupGPU(cluster.GPU, meta)
	card := &types.RateCard{
		Currency:                     file.Currency,
		Distribution:                 cluster.Distribution,
		MarkupBasisPoints:            int64(markup * 100),
		CPUMicroCentsPerCoreHour:     dollarsToMicroCents(cpuDollars),
		MemMicroCentsPerGiBHour:      types.RateMicroCentsPerGiBHour(cluster.Memory.DefaultDollarsPerGiBHour),
		StorageMicroCentsPerGiBMonth: types.RateMicroCentsPerGiBMonth(cluster.Storage.DefaultDollarsPerGiBMonth),
	}
	if cluster.GPU.DollarsPerGPUHour > 0 {
		card.GPUMicroCentsPerGPUHour = dollarsToMicroCents(cluster.GPU.DollarsPerGPUHour)
	} else if gpuMonth > 0 && hoursPerMonth > 0 {
		card.GPUMicroCentsPerGPUHour = types.RateMicroCentsPerDollarMonth(gpuMonth) / hoursPerMonth
	}
	return card, nil
}

func lookupCPU(cpu cpuRates, meta csv.RowMeta) float64 {
	if meta.InstanceType != "" {
		if v, ok := cpu.ByInstanceType[meta.InstanceType]; ok {
			return v
		}
	}
	if meta.Arch != "" {
		if v, ok := cpu.ByArchitecture[meta.Arch]; ok {
			return v
		}
	}
	return cpu.DefaultDollarsPerCoreHour
}

func lookupGPU(gpu gpuRates, meta csv.RowMeta) float64 {
	if meta.GPUModel != "" {
		if v, ok := gpu.ByModel[meta.GPUModel]; ok {
			return v
		}
	}
	if gpu.DefaultDollarsPerGPUMonth > 0 {
		return gpu.DefaultDollarsPerGPUMonth
	}
	return 0
}

func dollarsToMicroCents(usd float64) int64 {
	if usd <= 0 {
		return 0
	}
	return int64(math.Round(usd * float64(types.MicroCentsPerDollar)))
}
