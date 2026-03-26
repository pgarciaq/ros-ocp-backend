package kruize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/types/kruizePayload"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils/settings"
)

const kruizeUpdateExperimentPath = "/updateExperiment"

type updateExperimentPayload struct {
	ExperimentName         string                          `json:"experiment_name"`
	RecommendationSettings map[string]string               `json:"recommendation_settings"`
	TermSettings           *kruizePayload.TermSettings     `json:"term_settings,omitempty"`
	BusinessHours          *kruizePayload.BusinessHoursSettings `json:"business_hours,omitempty"`
}

// UpdateExperiment POSTs term/business-hours updates to Kruize for an existing experiment.
// 404 responses are treated as success (experiment not found). 5xx and network errors return an error.
func UpdateExperiment(kruizeURL, experimentName string, config *settings.CustomTimeframesResponse) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}

	base := strings.TrimRight(kruizeURL, "/")
	url := base + kruizeUpdateExperimentPath

	termSettings := settings.BuildTermSettingsForKruize(config)
	businessHours := toKruizeBusinessHours(&config.BusinessHours)

	payload := []updateExperimentPayload{{
		ExperimentName:         experimentName,
		RecommendationSettings: map[string]string{"threshold": "0.1"},
		TermSettings:           termSettings,
		BusinessHours:          businessHours,
	}}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal updateExperiment payload: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode >= 500 {
		_, _ = io.Copy(io.Discard, res.Body)
		return fmt.Errorf("kruize updateExperiment returned status %d", res.StatusCode)
	}
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, res.Body)
		return fmt.Errorf("kruize updateExperiment returned status %d", res.StatusCode)
	}
	return nil
}

func toKruizeBusinessHours(b *settings.BusinessHoursConfig) *kruizePayload.BusinessHoursSettings {
	if b == nil {
		return nil
	}
	return &kruizePayload.BusinessHoursSettings{
		Enabled:   b.Enabled,
		StartTime: b.StartTime,
		EndTime:   b.EndTime,
		Weekdays:  b.Weekdays,
		Timezone:  b.Timezone,
	}
}
