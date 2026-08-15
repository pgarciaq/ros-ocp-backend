package vm

// ClusterPreferenceRecord is one VirtualMachineClusterPreference from the operator catalog.
type ClusterPreferenceRecord struct {
	Name  string `json:"name"`
	Class string `json:"class"`
}

// VMPreferenceContext maps cluster preferences to ROS series names for recommendation matching.
type VMPreferenceContext struct {
	ClassByPreferenceName map[string]string
	VMToPreferenceName    map[string]string
}

// SeriesForVM returns the preferred instance type series for a VM, or fallbackSeries when unset.
func (ctx *VMPreferenceContext) SeriesForVM(namespace, vmName string, fallbackSeries string) string {
	if ctx == nil || len(ctx.VMToPreferenceName) == 0 {
		return fallbackSeries
	}
	prefName, ok := ctx.VMToPreferenceName[namespace+"/"+vmName]
	if !ok || prefName == "" {
		return fallbackSeries
	}
	if ctx.ClassByPreferenceName == nil {
		return fallbackSeries
	}
	if class, ok := ctx.ClassByPreferenceName[prefName]; ok {
		if series := NormalizePreferenceClass(class); series != "" {
			return series
		}
	}
	return fallbackSeries
}

// PreferenceInfoForVM returns the VM's preference name and raw class label when configured.
func (ctx *VMPreferenceContext) PreferenceInfoForVM(namespace, vmName string) (name, class string) {
	if ctx == nil {
		return "", ""
	}
	prefName, ok := ctx.VMToPreferenceName[namespace+"/"+vmName]
	if !ok {
		return "", ""
	}
	class = ""
	if ctx.ClassByPreferenceName != nil {
		class = ctx.ClassByPreferenceName[prefName]
	}
	return prefName, class
}

// NormalizePreferenceClass maps KubeVirt preference class labels to ROS series names.
func NormalizePreferenceClass(class string) string {
	switch class {
	case "compute-intensive", "compute":
		return vmSeriesComputeOptimized
	case "memory-intensive", "memory":
		return vmSeriesMemoryOptimized
	case "general-purpose", "":
		return vmSeriesGeneralPurpose
	default:
		return ""
	}
}

// BuildVMPreferenceContext maps preference catalog rows to a matching context.
func BuildVMPreferenceContext(prefs []ClusterPreferenceRecord, vmPrefs map[string]string) *VMPreferenceContext {
	if len(prefs) == 0 && len(vmPrefs) == 0 {
		return nil
	}
	ctx := &VMPreferenceContext{
		ClassByPreferenceName: make(map[string]string, len(prefs)),
		VMToPreferenceName:    vmPrefs,
	}
	for _, p := range prefs {
		if p.Name == "" {
			continue
		}
		ctx.ClassByPreferenceName[p.Name] = p.Class
	}
	if len(ctx.ClassByPreferenceName) == 0 && len(ctx.VMToPreferenceName) == 0 {
		return nil
	}
	if ctx.VMToPreferenceName == nil {
		ctx.VMToPreferenceName = map[string]string{}
	}
	return ctx
}

func buildVMPreferenceContext(prefs []ClusterPreferenceRecord, vmPrefs map[string]string) *VMPreferenceContext {
	return BuildVMPreferenceContext(prefs, vmPrefs)
}
