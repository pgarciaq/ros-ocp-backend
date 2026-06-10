package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/datatypes"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	kruizeplugin "github.com/redhatinsights/ros-ocp-backend/internal/plugins/kruize"
	"github.com/redhatinsights/ros-ocp-backend/internal/types/kruizePayload"
)

// escapeILIKE escapes ILIKE wildcard characters so user input is matched literally.
func escapeILIKE(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func CollectionResponse(collection []interface{}, req *http.Request, count, limit, offset int) *Collection {
	var first, previous, next, last string
	q := req.URL.Query()

	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(0))
	params, _ := url.PathUnescape(q.Encode())
	first = fmt.Sprintf("%v?%v", req.URL.Path, params)

	lastOffset := 0
	if count > 0 && limit > 0 {
		lastOffset = ((count - 1) / limit) * limit
	}
	q.Set("offset", strconv.Itoa(lastOffset))
	params, _ = url.PathUnescape(q.Encode())
	last = fmt.Sprintf("%v?%v", req.URL.Path, params)

	if offset-limit >= 0 {
		q.Set("offset", strconv.Itoa(offset-limit))
		params, _ = url.PathUnescape(q.Encode())
		previous = fmt.Sprintf("%v?%v", req.URL.Path, params)
	}

	if offset+limit < count {
		q.Set("offset", strconv.Itoa(offset+limit))
		params, _ = url.PathUnescape(q.Encode())
		next = fmt.Sprintf("%v?%v", req.URL.Path, params)
	}

	// set offset based on limit size aka page size
	links := Links{
		First:    first,
		Previous: previous,
		Next:     next,
		Last:     last,
	}

	return &Collection{
		Data: collection,
		Meta: Metadata{
			Count:  count,
			Limit:  limit,
			Offset: offset,
		},
		Links: links,
	}
}

// PaginatedCollectionResponse builds a list response with has_next and optional cursor metadata.
func PaginatedCollectionResponse(collection []interface{}, req *http.Request, count, limit, offset int, hasNext bool, nextCursor string) *Collection {
	resp := CollectionResponse(collection, req, count, limit, offset)
	resp.Meta.HasNext = hasNext
	if nextCursor != "" {
		resp.Meta.NextCursor = nextCursor
		if hasNext {
			q := req.URL.Query()
			q.Set("limit", strconv.Itoa(limit))
			q.Del("offset")
			q.Set("after", nextCursor)
			params, _ := url.PathUnescape(q.Encode())
			resp.Links.Next = fmt.Sprintf("%v?%v", req.URL.Path, params)
		}
	}
	return resp
}

func buildLinks(req *http.Request, count, limit, offset int) Links {
	q := req.URL.Query()
	q.Set("limit", strconv.Itoa(limit))

	q.Set("offset", strconv.Itoa(0))
	params, _ := url.PathUnescape(q.Encode())
	first := fmt.Sprintf("%v?%v", req.URL.Path, params)

	lastOffset := 0
	if count > 0 && limit > 0 {
		lastOffset = ((count - 1) / limit) * limit
	}
	q.Set("offset", strconv.Itoa(lastOffset))
	params, _ = url.PathUnescape(q.Encode())
	last := fmt.Sprintf("%v?%v", req.URL.Path, params)

	var previous, next string
	if offset-limit >= 0 {
		q.Set("offset", strconv.Itoa(offset-limit))
		params, _ = url.PathUnescape(q.Encode())
		previous = fmt.Sprintf("%v?%v", req.URL.Path, params)
	}
	if offset+limit < count {
		q.Set("offset", strconv.Itoa(offset+limit))
		params, _ = url.PathUnescape(q.Encode())
		next = fmt.Sprintf("%v?%v", req.URL.Path, params)
	}

	return Links{
		First:    first,
		Previous: previous,
		Next:     next,
		Last:     last,
	}
}

func MapQueryParameters(c echo.Context) (map[string]interface{}, error) {
	log := logging.GetLogger()
	queryParams := make(map[string]interface{})
	var startTimestamp, endTimestamp time.Time

	now := time.Now().UTC().Truncate(time.Second)
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	startDateStr := c.QueryParam("start_date")

	if startDateStr == "" {
		startTimestamp = firstOfMonth
	} else {
		var err error
		startTimestamp, err = time.Parse(timeLayout, startDateStr)
		if err != nil {
			log.Error("error parsing start_date:", err)
			return queryParams, err
		}
	}
	queryParams["recommendation_sets.monitoring_end_time >= ?"] = startTimestamp

	endDateStr := c.QueryParam("end_date")
	if endDateStr == "" {
		endTimestamp = now
	} else {
		var err error
		endTimestamp, err = time.Parse(timeLayout, endDateStr)
		if err != nil {
			log.Error("error parsing end_date:", err)
			return queryParams, err
		}
		// Inclusive user-provided end_date timestamp
		endTimestamp = endTimestamp.Add(24 * time.Hour)
	}
	queryParams["recommendation_sets.monitoring_end_time < ?"] = endTimestamp

	var errs []error
	if err := applyParamFilter(c, queryParams, "cluster", "", model.ClusterMaxLen, true, SkipSanitizationForContainer); err != nil {
		errs = append(errs, err)
	}
	if err := applyParamFilter(c, queryParams, "project", "workloads.namespace", model.NamespaceMaxLen, false, SkipSanitizationForContainer); err != nil {
		errs = append(errs, err)
	}
	if err := applyParamFilter(c, queryParams, "workload", "workloads.workload_name", model.ClusterMaxLen, true, SkipSanitizationForContainer); err != nil {
		errs = append(errs, err)
	}
	workloadTypeVals := queryparams.AllFilterValues(c, "workload_type")
	if err := validateWorkloadTypeValues(workloadTypeVals); err != nil {
		errs = append(errs, err)
	} else if err := applyParamFilter(c, queryParams, "workload_type", "workloads.workload_type", model.NamespaceMaxLen, false, SkipSanitizationForContainer, true); err != nil {
		errs = append(errs, err)
	}
	if err := applyParamFilter(c, queryParams, "container", "recommendation_sets.container_name", model.NamespaceMaxLen, false, SkipSanitizationForContainer); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return queryParams, errors.Join(errs...)
	}

	return queryParams, nil
}

func ParseUnitParams(c echo.Context, defaultCPU, defaultMemory string) (map[string]string, bool, error) {
	unitChoices := make(map[string]string)

	cpuUnitParam := c.QueryParam("cpu-unit")
	cpuUnitOptions := map[string]bool{
		"millicores": true,
		"cores":      true,
	}

	if cpuUnitParam != "" {
		if !cpuUnitOptions[cpuUnitParam] {
			return nil, false, fmt.Errorf("invalid cpu unit")
		}
		unitChoices["cpu"] = cpuUnitParam
	} else {
		unitChoices["cpu"] = defaultCPU
	}

	memoryUnitParam := c.QueryParam("memory-unit")
	memoryUnitOptions := map[string]bool{
		"bytes": true,
		"MiB":   true,
		"GiB":   true,
	}

	if memoryUnitParam != "" {
		if !memoryUnitOptions[memoryUnitParam] {
			return nil, false, fmt.Errorf("invalid memory unit")
		}
		unitChoices["memory"] = memoryUnitParam
	} else {
		unitChoices["memory"] = defaultMemory
	}

	trueUnitsStr := c.QueryParam("true-units")
	var trueUnits bool
	if trueUnitsStr != "" {
		var err error
		trueUnits, err = strconv.ParseBool(trueUnitsStr)
		if err != nil {
			return nil, false, fmt.Errorf("invalid value for true-units")
		}
	}

	return unitChoices, !trueUnits, nil
}

// isCharSafeRFC1123 returns true for chars valid in RFC 1123 DNS labels/subdomains, plus underscore.
// allowDot: true for subdomains (cluster alias), false for single labels (namespace).
// Additionally, isCharSafeRFC1123 aims to provide necessary defense from SQL injection attacks.
// Ref - https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
func isCharSafeRFC1123(c rune, allowDot bool) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '-', c == '_':
		return true
	case allowDot && c == '.':
		return true
	default:
		return false
	}
}

func sanitizeParamValue(paramName, s string, paramMaxLen int, allowDot bool, skipSanitize bool) (string, error) {
	if skipSanitize {
		return s, nil
	}
	if s == "" {
		return "", namespaceAPIErrf(EnableUserAPIErr, "empty value for %s", paramName)
	}
	if len(s) > paramMaxLen {
		return "", namespaceAPIErrf(EnableUserAPIErr, "%s exceeds max length %d", paramName, paramMaxLen)
	}
	for _, c := range s {
		if !isCharSafeRFC1123(c, allowDot) {
			return "", namespaceAPIErrf(EnableUserAPIErr, "invalid character in %s value", paramName)
		}
	}
	return s, nil
}

func parseClusterParams(value string, mode string) ([]string, []string, error) {
	if value == "" {
		return nil, nil, nil
	}
	modeClause := FilterModeClause[mode]
	if modeClause.Suffix == "" {
		return nil, nil, namespaceAPIErrf(EnableUserAPIErr, "unknown cluster filter mode: %s", mode)
	}
	if _, err := uuid.Parse(value); err == nil {
		suffix := modeClause.Suffix
		// for cluster_uuid exact is set for includes
		if mode == FilterModeInclude {
			suffix = FilterModeClause[FilterModeExact].Suffix
		}
		return []string{"clusters.cluster_uuid" + suffix}, []string{value}, nil
	}
	s := value
	if modeClause.Wrap {
		s = "%" + escapeILIKE(s) + "%"
	}
	return []string{"clusters.cluster_alias" + modeClause.Suffix}, []string{s}, nil
}

func buildModeClause(param, column, mode string, vals []string, maxLen int, allowDot bool, skipSanitize bool) (map[string]any, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	modeClause := FilterModeClause[mode]
	if modeClause.Suffix == "" {
		return nil, namespaceAPIErrf(EnableUserAPIErr, "unknown filter mode: %s", mode)
	}

	allSQLClauses := make([]string, 0, len(vals))
	allParamVals := make([]string, 0, len(vals))
	for _, val := range vals {
		if val == "" {
			continue
		}
		switch param {
		case "cluster":
			sqlClauses, paramVals, err := parseClusterParams(val, mode)
			if err != nil {
				return nil, err
			}
			allSQLClauses = append(allSQLClauses, sqlClauses...)
			allParamVals = append(allParamVals, paramVals...)
		default:
			s, err := sanitizeParamValue(param, val, maxLen, allowDot, skipSanitize)
			if err != nil {
				return nil, err
			}
			if param == "workload_type" {
				s = strings.ToLower(s)
			}
			if modeClause.Wrap {
				s = "%" + escapeILIKE(s) + "%"
			}
			allParamVals = append(allParamVals, s)
			allSQLClauses = append(allSQLClauses, workloadTypeSQLColumn(param, column, modeClause.Suffix))
		}
	}
	if len(allSQLClauses) == 0 {
		return nil, nil
	}
	joinedSQLClause := strings.Join(allSQLClauses, modeClause.Join)
	return map[string]any{joinedSQLClause: allParamVals}, nil
}

func workloadTypeSQLColumn(param, column, suffix string) string {
	if param == "workload_type" {
		return "LOWER(" + column + ")" + suffix
	}
	return column + suffix
}

// parsing of string params based on mode -> include, exclude, exact.
func buildSQLClauseWithFilterType(
	param string,
	includeVals,
	exactVals,
	excludeVals []string,
	column string,
	maxLen int,
	allowDot bool,
	skipSanitize bool,
) (map[string]any, error) {
	hasExclude, hasExact, hasInclude := len(excludeVals) > 0, len(exactVals) > 0, len(includeVals) > 0

	if !hasExclude && !hasExact {
		if !hasInclude {
			return nil, nil
		}
		// early exit as default is includes i.e. param=value
		return buildModeClause(param, column, FilterModeInclude, includeVals, maxLen, allowDot, skipSanitize)
	}

	if hasExclude {
		for _, ev := range excludeVals {
			if slices.Contains(exactVals, ev) {
				return nil, namespaceAPIErrf(EnableUserAPIErr, "exclude and exact cannot share values for %s", param)
			}
			if slices.Contains(includeVals, ev) {
				return nil, namespaceAPIErrf(EnableUserAPIErr, "exclude and include cannot share values for %s", param)
			}
		}
	}

	clauseMap := make(map[string]any)
	if len(excludeVals) > 0 {
		clause, err := buildModeClause(param, column, FilterModeExclude, excludeVals, maxLen, allowDot, skipSanitize)
		if err != nil {
			return nil, err
		}
		if clause != nil {
			maps.Copy(clauseMap, clause)
		}
	}
	if len(exactVals) > 0 {
		clause, err := buildModeClause(param, column, FilterModeExact, exactVals, maxLen, allowDot, skipSanitize)
		if err != nil {
			return nil, err
		}
		if clause != nil {
			maps.Copy(clauseMap, clause)
		}
	}
	// exact is priority when present with includes for the same value
	var includeValsFiltered []string
	if hasExact && hasInclude {
		exactSet := make(map[string]bool)
		for _, v := range exactVals {
			exactSet[v] = true
		}
		for _, v := range includeVals {
			if !exactSet[v] {
				includeValsFiltered = append(includeValsFiltered, v)
			}
		}
	} else {
		includeValsFiltered = includeVals
	}
	if len(includeValsFiltered) > 0 {
		clause, err := buildModeClause(param, column, FilterModeInclude, includeValsFiltered, maxLen, allowDot, skipSanitize)
		if err != nil {
			return nil, err
		}
		if clause != nil {
			maps.Copy(clauseMap, clause)
		}
	}
	return clauseMap, nil
}

func applyParamFilter(
	c echo.Context,
	queryParams map[string]any,
	param, column string,
	maxLen int,
	allowDot bool,
	skipSanitize bool, //nolint:unparam
	treatIncludeAsExact ...bool,
) error {
	cfg := config.GetConfig()
	useExactForInclude := len(treatIncludeAsExact) > 0 && treatIncludeAsExact[0]
	var includeVals, excludeVals, exactVals []string
	excludeVals = queryparams.ExcludeValues(c, param)
	exactFromBracket := queryparams.ExactValues(c, param)
	if useExactForInclude {
		exactVals = append(queryparams.IncludeValues(c, param), exactFromBracket...)
	} else {
		includeVals = queryparams.IncludeValues(c, param)
		exactVals = exactFromBracket
	}

	if len(includeVals) > cfg.MaxCountPerQueryParam {
		return namespaceAPIErrf(EnableUserAPIErr, "too many %s parameters, a maximum of %d is allowed", param, cfg.MaxCountPerQueryParam)
	}

	if len(excludeVals) > cfg.MaxCountPerQueryParam {
		return namespaceAPIErrf(EnableUserAPIErr, "too many %s parameters, a maximum of %d is allowed", param, cfg.MaxCountPerQueryParam)
	}

	if len(exactVals) > cfg.MaxCountPerQueryParam {
		return namespaceAPIErrf(EnableUserAPIErr, "too many %s parameters, a maximum of %d is allowed", param, cfg.MaxCountPerQueryParam)
	}

	if len(includeVals) == 0 && len(excludeVals) == 0 && len(exactVals) == 0 {
		return nil
	}
	clauseMap, err := buildSQLClauseWithFilterType(param, includeVals, exactVals, excludeVals, column, maxLen, allowDot, skipSanitize)
	if err != nil {
		return err
	}
	if clauseMap != nil {
		maps.Copy(queryParams, clauseMap)
	}
	return nil
}

func MapNamespaceQueryParameters(c echo.Context) (map[string]any, error) {
	log := logging.GetLogger()
	queryParams := make(map[string]any)
	var startTimestamp, endTimestamp time.Time

	now := time.Now().UTC().Truncate(time.Second)
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	startDateStr := c.QueryParam("start_date")
	if startDateStr == "" {
		startTimestamp = firstOfMonth
	} else {
		var err error
		startTimestamp, err = time.Parse(timeLayout, startDateStr)
		if err != nil {
			log.Error("error parsing start_date:", err)
			return queryParams, namespaceAPIErrf(EnableUserAPIErr, "invalid start_date format, use YYYY-MM-DD")
		}
	}
	queryParams["namespace_recommendation_sets.monitoring_end_time >= ?"] = startTimestamp

	endDateStr := c.QueryParam("end_date")
	if endDateStr == "" {
		endTimestamp = now
	} else {
		var err error
		endTimestamp, err = time.Parse(timeLayout, endDateStr)
		if err != nil {
			log.Error("error parsing end_date:", err)
			return queryParams, namespaceAPIErrf(EnableUserAPIErr, "invalid end_date format, use YYYY-MM-DD")
		}
		endTimestamp = endTimestamp.Add(24 * time.Hour)
	}
	queryParams["namespace_recommendation_sets.monitoring_end_time < ?"] = endTimestamp

	var errs []error
	if err := applyParamFilter(c, queryParams, "cluster", "", model.ClusterMaxLen, true, SkipSanitizationForNamespace); err != nil {
		errs = append(errs, err)
	}
	if err := applyParamFilter(c, queryParams, "project", "namespace_recommendation_sets.namespace_name", model.NamespaceMaxLen, false, SkipSanitizationForNamespace); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return queryParams, errors.Join(errs...)
	}
	if err := attachTagFiltersToQueryParams(c, queryParams); err != nil {
		return queryParams, err
	}

	return queryParams, nil
}

// parseNativeClusterParams handles cluster filter with mode support for native
// queries that use the "c." table alias instead of "clusters.".
func parseNativeClusterParams(value string, mode string) ([]string, []string, error) {
	if value == "" {
		return nil, nil, nil
	}
	modeClause := FilterModeClause[mode]
	if modeClause.Suffix == "" {
		return nil, nil, namespaceAPIErrf(false, "unknown cluster filter mode: %s", mode)
	}
	if _, err := uuid.Parse(value); err == nil {
		suffix := modeClause.Suffix
		if mode == FilterModeInclude {
			suffix = FilterModeClause[FilterModeExact].Suffix
		}
		return []string{"c.cluster_uuid" + suffix}, []string{value}, nil
	}
	s, err := sanitizeParamValue("cluster", value, model.ClusterMaxLen, true, false)
	if err != nil {
		return nil, nil, err
	}
	if modeClause.Wrap {
		s = "%" + escapeILIKE(s) + "%"
	}
	return []string{"c.cluster_alias" + modeClause.Suffix}, []string{s}, nil
}

// buildNativeModeClause builds SQL clause with mode support for native queries.
// Uses parseNativeClusterParams for cluster params, standard logic for others.
func buildNativeModeClause(param, column, mode string, vals []string, maxLen int, allowDot bool, skipSanitize bool) (map[string]any, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	modeClause := FilterModeClause[mode]
	if modeClause.Suffix == "" {
		return nil, namespaceAPIErrf(false, "unknown filter mode: %s", mode)
	}

	allSQLClauses := make([]string, 0, len(vals))
	allParamVals := make([]string, 0, len(vals))
	for _, val := range vals {
		if val == "" {
			continue
		}
		switch param {
		case "cluster":
			sqlClauses, paramVals, err := parseNativeClusterParams(val, mode)
			if err != nil {
				return nil, err
			}
			allSQLClauses = append(allSQLClauses, sqlClauses...)
			allParamVals = append(allParamVals, paramVals...)
		default:
			s, err := sanitizeParamValue(param, val, maxLen, allowDot, skipSanitize)
			if err != nil {
				return nil, err
			}
			if param == "workload_type" {
				s = strings.ToLower(s)
			}
			if modeClause.Wrap {
				s = "%" + escapeILIKE(s) + "%"
			}
			allParamVals = append(allParamVals, s)
			allSQLClauses = append(allSQLClauses, workloadTypeSQLColumn(param, column, modeClause.Suffix))
		}
	}
	if len(allSQLClauses) == 0 {
		return nil, nil
	}
	joinedSQLClause := strings.Join(allSQLClauses, modeClause.Join)
	return map[string]any{joinedSQLClause: allParamVals}, nil
}

// buildNativeSQLClauseWithFilterType handles include/exclude/exact for native queries.
func buildNativeSQLClauseWithFilterType(param string, includeVals, exactVals, excludeVals []string, column string, maxLen int, allowDot bool, skipSanitize bool, treatIncludeAsExact bool) (map[string]any, error) {
	hasExclude, hasExact, hasInclude := len(excludeVals) > 0, len(exactVals) > 0, len(includeVals) > 0

	if treatIncludeAsExact {
		exactVals = append(includeVals, exactVals...)
		includeVals = nil
		hasInclude = false
		hasExact = len(exactVals) > 0
	}

	if !hasExclude && !hasExact {
		if !hasInclude {
			return nil, nil
		}
		return buildNativeModeClause(param, column, FilterModeInclude, includeVals, maxLen, allowDot, skipSanitize)
	}

	if hasExclude {
		for _, ev := range excludeVals {
			if slices.Contains(exactVals, ev) {
				return nil, namespaceAPIErrf(true, "exclude and exact cannot share values for %s", param)
			}
			if slices.Contains(includeVals, ev) {
				return nil, namespaceAPIErrf(true, "exclude and include cannot share values for %s", param)
			}
		}
	}

	clauseMap := make(map[string]any)
	if len(excludeVals) > 0 {
		clause, err := buildNativeModeClause(param, column, FilterModeExclude, excludeVals, maxLen, allowDot, skipSanitize)
		if err != nil {
			return nil, err
		}
		if clause != nil {
			maps.Copy(clauseMap, clause)
		}
	}
	if len(exactVals) > 0 {
		clause, err := buildNativeModeClause(param, column, FilterModeExact, exactVals, maxLen, allowDot, skipSanitize)
		if err != nil {
			return nil, err
		}
		if clause != nil {
			maps.Copy(clauseMap, clause)
		}
	}
	var includeValsFiltered []string
	if hasExact && hasInclude {
		exactSet := make(map[string]bool)
		for _, v := range exactVals {
			exactSet[v] = true
		}
		for _, v := range includeVals {
			if !exactSet[v] {
				includeValsFiltered = append(includeValsFiltered, v)
			}
		}
	} else {
		includeValsFiltered = includeVals
	}
	if len(includeValsFiltered) > 0 {
		clause, err := buildNativeModeClause(param, column, FilterModeInclude, includeValsFiltered, maxLen, allowDot, skipSanitize)
		if err != nil {
			return nil, err
		}
		if clause != nil {
			maps.Copy(clauseMap, clause)
		}
	}
	return clauseMap, nil
}

func applyNativeParamFilter(
	c echo.Context,
	queryParams map[string]any,
	param, column string,
	maxLen int,
	allowDot bool,
	skipSanitize bool,
	treatIncludeAsExact ...bool,
) error {
	cfg := config.GetConfig()
	includeVals := queryparams.IncludeValues(c, param)
	excludeVals := queryparams.ExcludeValues(c, param)
	exactVals := queryparams.ExactValues(c, param)
	if len(includeVals) > cfg.MaxCountPerQueryParam {
		return namespaceAPIErrf(true, "too many %s parameters, a maximum of %d is allowed", param, cfg.MaxCountPerQueryParam)
	}
	if len(excludeVals) > cfg.MaxCountPerQueryParam {
		return namespaceAPIErrf(true, "too many %s parameters, a maximum of %d is allowed", param, cfg.MaxCountPerQueryParam)
	}
	if len(exactVals) > cfg.MaxCountPerQueryParam {
		return namespaceAPIErrf(true, "too many %s parameters, a maximum of %d is allowed", param, cfg.MaxCountPerQueryParam)
	}
	if len(includeVals) == 0 && len(excludeVals) == 0 && len(exactVals) == 0 {
		return nil
	}
	useExactForInclude := len(treatIncludeAsExact) > 0 && treatIncludeAsExact[0]
	clauseMap, err := buildNativeSQLClauseWithFilterType(param, includeVals, exactVals, excludeVals, column, maxLen, allowDot, skipSanitize, useExactForInclude)
	if err != nil {
		return err
	}
	if clauseMap != nil {
		maps.Copy(queryParams, clauseMap)
	}
	return nil
}

// MapNativeNamespaceQueryParameters parses namespace query params using the
// native schema's column names (ns.* aliases from namespace_recommendation_sets).
// Supports include, exclude, and exact filter modes for cluster and project params.
func MapNativeNamespaceQueryParameters(c echo.Context) (map[string]interface{}, error) {
	queryParams := make(map[string]interface{})
	var startTimestamp, endTimestamp time.Time

	now := time.Now().UTC()
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	startDateStr := c.QueryParam("start_date")
	if startDateStr == "" {
		startTimestamp = firstOfMonth
	} else {
		var err error
		startTimestamp, err = time.Parse(timeLayout, startDateStr)
		if err != nil {
			return queryParams, namespaceAPIErrf(true, "invalid start_date format, use YYYY-MM-DD")
		}
	}
	queryParams["ns.monitoring_end_time >= ?"] = startTimestamp

	endDateStr := c.QueryParam("end_date")
	if endDateStr == "" {
		endTimestamp = now.Add(time.Second)
	} else {
		var err error
		endTimestamp, err = time.Parse(timeLayout, endDateStr)
		if err != nil {
			return queryParams, namespaceAPIErrf(true, "invalid end_date format, use YYYY-MM-DD")
		}
		endTimestamp = endTimestamp.Add(24 * time.Hour)
	}
	queryParams["ns.monitoring_end_time < ?"] = endTimestamp

	var errs []error
	if err := applyNativeParamFilter(c, queryParams, "cluster", "", model.ClusterMaxLen, true, false); err != nil {
		errs = append(errs, err)
	}
	if err := applyNativeParamFilter(c, queryParams, "project", "ns.namespace_name", model.NamespaceMaxLen, false, false); err != nil {
		errs = append(errs, err)
	}
	if err := attachTagFiltersToQueryParams(c, queryParams); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return queryParams, errors.Join(errs...)
	}

	applyRecommendationStaleFilter(c, queryParams, "ns")

	if idleVals := queryparams.IncludeValues(c, "idle_state"); len(idleVals) > 0 {
		states, err := model.IdleStateFilterValues(strings.Join(idleVals, ","))
		if err != nil {
			errs = append(errs, err)
		} else if len(states) > 0 {
			queryParams["ns.idle_state IN ?"] = states
		}
	}

	if err := applyNativeEngineQueryFilter(c, queryParams, "ns.engine"); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return queryParams, errors.Join(errs...)
	}

	return queryParams, nil
}

// applyRecommendationStaleFilter adds a stale column predicate from filter[stale].
// columnPrefix is the SQL table alias (rs or ns).
func applyRecommendationStaleFilter(c echo.Context, queryParams map[string]interface{}, columnPrefix string) {
	staleKey := columnPrefix + ".stale = ?"
	switch queryparams.FirstFilter(c, "stale") {
	case "true":
		// No filter — return both stale and non-stale.
	case "only":
		queryParams[staleKey] = true
	default:
		// "false" or unset: exclude stale (backward compatible).
		queryParams[staleKey] = false
	}
}

func get_user_permissions(c echo.Context) map[string][]string {
	var user_permissions map[string][]string
	switch t := c.Get("user.permissions").(type) {
	case map[string][]string:
		user_permissions = t
	default:
		user_permissions = map[string][]string{}
	}
	return user_permissions
}

func UpdateRecommendationJSON(handlerName string, recommendationID string, clusterUUID string, unitsToTransform map[string]string, updateUnitsk8s bool, jsonData datatypes.JSON, storedPcts *model.StoredVariationPcts) map[string]interface{} {
	return kruizeplugin.UpdateRecommendationJSON(handlerName, recommendationID, clusterUUID, unitsToTransform, updateUnitsk8s, jsonData, storedPcts)
}

func GenerateCSVRows(recommendationSet model.RecommendationSetResult) ([][]string, error) {
	rows := [][]string{}
	variationFormat := "percent"
	var recommendationObj kruizePayload.RecommendationData

	if recommendationSet.RecommendationsJSON == nil {
		return nil, fmt.Errorf("RecommendationsJSON not set for %s: call UpdateRecommendationJSON first", recommendationSet.ID)
	}
	b, err := json.Marshal(recommendationSet.RecommendationsJSON)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal RecommendationsJSON %s: %w", recommendationSet.ID, err)
	}
	if err := json.Unmarshal(b, &recommendationObj); err != nil {
		return nil, fmt.Errorf("unable to unmarshall recommendation %s: %w", recommendationSet.ID, err)
	}

	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

	type namedTerm struct {
		name string
		term kruizePayload.RecommendationTerm
	}
	orderedTerms := []namedTerm{
		{KruizeShortTerm, recommendationObj.RecommendationTerms.Short_term},
		{KruizeMediumTerm, recommendationObj.RecommendationTerms.Medium_term},
		{KruizeLongTerm, recommendationObj.RecommendationTerms.Long_term},
	}

	type namedEngine struct {
		name   string
		engine kruizePayload.RecommendationEngineObject
	}

	for _, nt := range orderedTerms {
		termName := nt.name
		recommendationTerm := nt.term
		if recommendationTerm.RecommendationEngines == nil {
			continue
		}
		orderedEngines := []namedEngine{
			{KruizeEngineCost, recommendationTerm.RecommendationEngines.Cost},
			{KruizeEnginePerformance, recommendationTerm.RecommendationEngines.Performance},
		}
		for _, ne := range orderedEngines {
			recommendationType := ne.name
			recommendationEngine := ne.engine
			rows = append(rows, []string{
				recommendationSet.ID,
				recommendationSet.ClusterUUID,
				recommendationSet.ClusterAlias,
				recommendationSet.Container,
				recommendationSet.Project,
				recommendationSet.Workload,
				recommendationSet.WorkloadType,
				recommendationSet.LastReported,
				recommendationSet.SourceID,
				f(kruizeplugin.ConvertCPUUnit("cores", recommendationObj.Current.Limits.Cpu.Amount)),
				recommendationObj.Current.Limits.Cpu.Format,
				f(recommendationObj.Current.Limits.Memory.Amount),
				recommendationObj.Current.Limits.Memory.Format,
				f(kruizeplugin.ConvertCPUUnit("cores", recommendationObj.Current.Requests.Cpu.Amount)),
				recommendationObj.Current.Requests.Cpu.Format,
				f(recommendationObj.Current.Requests.Memory.Amount),
				recommendationObj.Current.Requests.Memory.Format,
				recommendationObj.MonitoringEndTime.Format(time.RFC3339),
				termName,
				fmt.Sprint(recommendationTerm.DurationInHours),
				recommendationTerm.MonitoringStartTime.Format(time.RFC3339),
				recommendationType,
				f(kruizeplugin.ConvertCPUUnit("cores", recommendationEngine.Config.Limits.Cpu.Amount)),
				recommendationEngine.Config.Limits.Cpu.Format,
				f(recommendationEngine.Config.Limits.Memory.Amount),
				recommendationEngine.Config.Limits.Memory.Format,
				f(kruizeplugin.ConvertCPUUnit("cores", recommendationEngine.Config.Requests.Cpu.Amount)),
				recommendationEngine.Config.Requests.Cpu.Format,
				f(recommendationEngine.Config.Requests.Memory.Amount),
				recommendationEngine.Config.Requests.Memory.Format,
				f(recommendationEngine.Variation.Limits.Cpu.Amount),
				variationFormat,
				f(recommendationEngine.Variation.Limits.Memory.Amount),
				variationFormat,
				f(recommendationEngine.Variation.Requests.Cpu.Amount),
				variationFormat,
				f(recommendationEngine.Variation.Requests.Memory.Amount),
				variationFormat,
			})
		}
	}
	return rows, nil
}

// GenerateNativeCSV writes native recommendation results as CSV.
func GenerateNativeCSV(ctx context.Context, w io.Writer, results []model.NativeContainerResult) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(NativeCSVHeader); err != nil {
		return fmt.Errorf("unable to write header: %w", err)
	}

	for _, r := range results {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, termName := range []string{"short_term", "medium_term", "long_term"} {
			term, ok := r.Recommendations[termName]
			if !ok {
				continue
			}
			for _, eng := range []struct {
				name string
				rec  *model.EngineRecommendation
			}{
				{"cost", term.Cost},
				{"performance", term.Performance},
			} {
				if eng.rec == nil {
					continue
				}
				pcMin, pcMax, pcAvg := "", "", ""
				if r.Replicas != nil {
					pcMin = strconv.Itoa(r.Replicas.Min)
					pcMax = strconv.Itoa(r.Replicas.Max)
					pcAvg = strconv.Itoa(r.Replicas.Avg)
				}
				row := []string{
					r.ClusterUUID,
					r.ClusterAlias,
					r.Container,
					r.Project,
					r.Workload,
					r.WorkloadType,
					r.LastReported,
					r.SourceID,
					pcMin,
					pcMax,
					pcAvg,
					optionalSavingsStr(r.EstimatedMonthlySavings),
					optionalSavingsStr(r.EstimatedMonthlyWaste),
					optionalSavingsCurrencyStr(r.EstimatedMonthlyWaste, r.Currency),
					optionalSavingsCurrencyStr(r.EstimatedMonthlySavings, r.Currency),
					r.IdleState,
					optionalIdleSinceStr(r.IdleSince),
					optionalIntPtrStr(r.IdleDurationDays),
					optionalInt64Str(r.PeakCPUMillicores),
					optionalInt64Str(r.PeakMemoryBytes),
					termName,
					eng.name,
					optionalInt64Str(eng.rec.CPURequestMillicores),
					optionalInt64Str(eng.rec.CPULimitMillicores),
					optionalInt64Str(eng.rec.MemRequestKiB),
					optionalInt64Str(eng.rec.MemLimitKiB),
					optionalInt64Str(eng.rec.CurrentCPURequestMC),
					optionalInt64Str(eng.rec.CurrentCPULimitMC),
					optionalInt64Str(eng.rec.CurrentMemRequestKiB),
					optionalInt64Str(eng.rec.CurrentMemLimitKiB),
					optionalInt32Str(eng.rec.VariationCPURequestPct),
					optionalInt32Str(eng.rec.VariationCPULimitPct),
					optionalInt32Str(eng.rec.VariationMemRequestPct),
					optionalInt32Str(eng.rec.VariationMemLimitPct),
					optionalFloat32Str(eng.rec.ConfidenceLevel),
					int16SliceStr(eng.rec.NotificationCodes),
				}
				if err := writer.Write(row); err != nil {
					return fmt.Errorf("unable to write row: %w", err)
				}
			}
		}
	}

	writer.Flush()
	return writer.Error()
}

// GenerateNativeNamespaceCSV writes native namespace recommendation results as CSV.
func GenerateNativeNamespaceCSV(ctx context.Context, w io.Writer, results []model.NativeNamespaceResult) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(NativeNSCSVHeader); err != nil {
		return fmt.Errorf("unable to write header: %w", err)
	}

	for _, r := range results {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, termName := range []string{"short_term", "medium_term", "long_term"} {
			termAny, ok := r.Recommendations[termName]
			if !ok {
				continue
			}
			term, ok := termAny.(model.TermRecommendation)
			if !ok {
				continue
			}
			for _, eng := range []struct {
				name string
				rec  *model.EngineRecommendation
			}{
				{"cost", term.Cost},
				{"performance", term.Performance},
			} {
				if eng.rec == nil {
					continue
				}
				row := []string{
					r.ClusterUUID,
					r.ClusterAlias,
					r.Project,
					r.LastReported,
					r.SourceID,
					r.IdleState,
					termName,
					eng.name,
					optionalInt64Str(eng.rec.CPURequestMillicores),
					optionalInt64Str(eng.rec.CPULimitMillicores),
					optionalInt64Str(eng.rec.MemRequestKiB),
					optionalInt64Str(eng.rec.MemLimitKiB),
					optionalInt64Str(eng.rec.CurrentCPURequestMC),
					optionalInt64Str(eng.rec.CurrentCPULimitMC),
					optionalInt64Str(eng.rec.CurrentMemRequestKiB),
					optionalInt64Str(eng.rec.CurrentMemLimitKiB),
					optionalInt32Str(eng.rec.VariationCPURequestPct),
					optionalInt32Str(eng.rec.VariationCPULimitPct),
					optionalInt32Str(eng.rec.VariationMemRequestPct),
					optionalInt32Str(eng.rec.VariationMemLimitPct),
					optionalFloat32Str(eng.rec.ConfidenceLevel),
					int16SliceStr(eng.rec.NotificationCodes),
				}
				if err := writer.Write(row); err != nil {
					return fmt.Errorf("unable to write row: %w", err)
				}
			}
		}
	}

	writer.Flush()
	return writer.Error()
}

func optionalInt64Str(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

func optionalFloat32Str(v *float32) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(float64(*v), 'f', 3, 32)
}

func optionalSavingsStr(v *money.MoneyAmount) string {
	if v == nil {
		return ""
	}
	return v.Value
}

func optionalIdleSinceStr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func optionalIntPtrStr(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func optionalSavingsCurrencyStr(v *money.MoneyAmount, rowCurrency string) string {
	if v == nil {
		return ""
	}
	if v.Units != "" {
		return v.Units
	}
	if rowCurrency != "" {
		return rowCurrency
	}
	return money.DefaultCurrency
}

func optionalInt32Str(v *int32) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(int64(*v), 10)
}

func GenerateAndStreamCSV(ctx context.Context, w io.Writer, recommendationSets []model.RecommendationSetResult) error {
	writer := csv.NewWriter(w)
	header := FlattenedCSVHeader

	if err := writer.Write(header); err != nil {
		return fmt.Errorf("unable to write header: %w", err)
	}

	for i := range recommendationSets {
		if err := ctx.Err(); err != nil {
			return err
		}
		CSVRows, generateRowErr := GenerateCSVRows(recommendationSets[i])
		if generateRowErr != nil {
			return fmt.Errorf("unable to generate rows: %w", generateRowErr)
		}
		for _, row := range CSVRows {
			if err := writer.Write(row); err != nil {
				return fmt.Errorf("unable to write row: %w", err)
			}
		}

		if (i+1)%config.GetConfig().CSVStreamInterval == 0 { // flush every CSVStreamInterval db records
			writer.Flush()
			if err := writer.Error(); err != nil {
				return fmt.Errorf("periodic flush error at row %d: %w", i+1, err)
			}
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush error: %w", err)
	}
	return nil
}

// stripTagFiltersFromQueryParams removes parsed tag filters before legacy SQL query builders
// that iterate queryParams keys as column predicates.
func stripTagFiltersFromQueryParams(queryParams map[string]interface{}) {
	delete(queryParams, model.TagFiltersQueryKey)
}

// attachTagFiltersToQueryParams parses tag filters from the request when enabled and stores them on queryParams.
func attachTagFiltersToQueryParams(c echo.Context, queryParams map[string]interface{}) error {
	if !config.TagsFeatureEnabled() {
		return nil
	}
	tagFilters, err := parseTagFiltersFromRequest(c)
	if err != nil {
		return err
	}
	if len(tagFilters) > 0 {
		queryParams[model.TagFiltersQueryKey] = tagFilters
	}
	return nil
}

// parseTagFiltersFromRequest parses legacy ?tag=key:value and Koku ?filter[tag:key]=v1,v2 syntax.
func parseTagFiltersFromRequest(c echo.Context) ([]model.TagFilter, error) {
	kokuFilters, err := model.ParseKokuTagFilterParams(c.QueryParams())
	if err != nil {
		return nil, err
	}
	legacyFilters, err := model.ParseTagFilters(c.QueryParams()["tag"])
	if err != nil {
		return nil, err
	}
	return model.MergeTagFilters(append(kokuFilters, legacyFilters...)), nil
}

// apiErrResponse is the single gate for user-facing error responses.
// Always returns the typed `{"status":"error","message":"..."}` shape documented
// in the OpenAPI spec. The HTTP status code conveys severity; the message provides
// actionable context. Logs the full error internally.
func apiErrResponse(c echo.Context, err error, status int, userMsg string) error {
	log.Error(err.Error())
	return c.JSON(status, echo.Map{"status": "error", "message": userMsg})
}
