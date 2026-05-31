package engine

// TermWindow defines the lookback window for VM recommendation computation.
type TermWindow struct {
	Name         string // short_term, medium_term, long_term
	LookbackDays int
	MinDataDays  int
}

// DefaultVMTermWindows returns the default VM term windows.
func DefaultVMTermWindows() []TermWindow {
	return []TermWindow{
		{Name: "short_term", LookbackDays: 7, MinDataDays: 3},
		{Name: "medium_term", LookbackDays: 15, MinDataDays: 7},
		{Name: "long_term", LookbackDays: 30, MinDataDays: 15},
	}
}

// VMTermWindowsFromConfig converts engine term configs to VM term windows.
func VMTermWindowsFromConfig(terms []TermConfig) []TermWindow {
	if len(terms) == 0 {
		return DefaultVMTermWindows()
	}
	out := make([]TermWindow, len(terms))
	for i, tc := range terms {
		name := tc.Name
		switch name {
		case "short":
			name = "short_term"
		case "medium":
			name = "medium_term"
		case "long":
			name = "long_term"
		}
		out[i] = TermWindow{
			Name:         name,
			LookbackDays: tc.WindowDays,
			MinDataDays:  tc.MinDataDays,
		}
	}
	return out
}

// MaxVMLookbackDays returns the largest lookback across VM term windows.
func MaxVMLookbackDays(terms []TermWindow) int {
	max := 0
	for _, t := range terms {
		if t.LookbackDays > max {
			max = t.LookbackDays
		}
	}
	return max
}
