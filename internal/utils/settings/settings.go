package settings

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const rosCustomTimeframesPath = "/api/cost-management/v1/account-settings/ros-custom-timeframes/"

// CustomTimeframesResponse is the Koku Settings API shape for ROS custom timeframes.
type CustomTimeframesResponse struct {
	Terms         []TermConfig        `json:"terms"`
	BusinessHours BusinessHoursConfig `json:"business_hours"`
}

// TermConfig is one user-facing term (term1..term3) with duration in days.
type TermConfig struct {
	Name         string `json:"name"`
	DurationDays int    `json:"duration_days"`
}

// BusinessHoursConfig configures optional business-hours analysis in Kruize.
type BusinessHoursConfig struct {
	Enabled   bool   `json:"enabled"`
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
	Weekdays  []int  `json:"weekdays,omitempty"`
}

type kokuSettingsEnvelope struct {
	Data []CustomTimeframesResponse `json:"data"`
}

// DefaultCustomTimeframes returns the built-in defaults when Settings API is unavailable.
func DefaultCustomTimeframes() CustomTimeframesResponse {
	return CustomTimeframesResponse{
		Terms: []TermConfig{
			{Name: "term1", DurationDays: 1},
			{Name: "term2", DurationDays: 7},
			{Name: "term3", DurationDays: 15},
		},
		BusinessHours: BusinessHoursConfig{Enabled: false},
	}
}

// FetchCustomTimeframes GETs ROS custom timeframes from Koku Settings API.
// On 404, 500, connection failure, empty baseURL, or parse errors, it returns defaults and a nil error.
// When identity is non-empty, sets x-rh-identity (SaaS); when empty, omits it (on-prem).
func FetchCustomTimeframes(baseURL, identity string) (CustomTimeframesResponse, error) {
	if strings.TrimSpace(baseURL) == "" {
		return DefaultCustomTimeframes(), nil
	}

	url := strings.TrimRight(baseURL, "/") + rosCustomTimeframesPath
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return DefaultCustomTimeframes(), nil
	}
	if identity != "" {
		req.Header.Set("x-rh-identity", identity)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return DefaultCustomTimeframes(), nil
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound || res.StatusCode >= 500 {
		return DefaultCustomTimeframes(), nil
	}

	if res.StatusCode != http.StatusOK {
		return DefaultCustomTimeframes(), nil
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return DefaultCustomTimeframes(), nil
	}

	var env kokuSettingsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return DefaultCustomTimeframes(), nil
	}
	if len(env.Data) == 0 {
		return DefaultCustomTimeframes(), nil
	}

	return env.Data[0], nil
}
