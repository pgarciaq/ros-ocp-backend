package kruize

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
	"gorm.io/datatypes"
)

var log = logging.GetLogger()

func ConvertCPUUnit(cpuUnit string, cpuValue float64) float64 {
	var convertedValueCPU float64

	switch cpuUnit {
	case "millicores":
		convertedValueCPU = math.Round(cpuValue * 1000) // millicore values don't require decimal precision
	case "cores":
		convertedValueCPU = utils.TruncateToThreeDecimalPlaces(cpuValue)
	default:
		convertedValueCPU = cpuValue
	}

	return convertedValueCPU
}

func ConvertMemoryUnit(memoryUnit string, memoryValue float64) float64 {
	var convertedValueMemory float64

	switch memoryUnit {
	case "MiB":
		convertedValueMemory = utils.TruncateMemoryBytesToMiBTwoDecimals(memoryValue)
	case "GiB":
		convertedValueMemory = utils.TruncateMemoryBytesToGiBTwoDecimals(memoryValue)
	case "bytes":
		convertedValueMemory = memoryValue
	}

	return convertedValueMemory
}

func transformComponentUnits(unitsToTransform map[string]string, updateUnitsk8s bool, recommendationJSON map[string]interface{}) map[string]interface{} {
	/*
		Truncates CPU units(cores) to three decimal places
		Truncates Memory units(Mi) to two decimal places
		Hack: Truncates duration_in_hours to one decimal places
		TODO: Once Kruize returns identical values for duration_in_hours
		the ros-ocp should stop truncating the duration_in_hours
	*/

	truncateDurationInHours := func(intervalData map[string]interface{}) bool {
		durationInHours, ok := intervalData["duration_in_hours"].(float64)
		if ok {
			intervalData["duration_in_hours"] = math.Trunc(durationInHours*10) / 10
		}
		return ok
	}

	// Current section of recommendation
	current_config, ok := recommendationJSON["current"].(map[string]interface{})
	if !ok {
		log.Error("current not found in JSON")
	}

	for _, section := range []string{"limits", "requests"} {
		sectionObject, ok := current_config[section].(map[string]interface{})
		if ok {
			memoryObject, ok := sectionObject["memory"].(map[string]interface{})
			if ok {
				if memoryValue, ok := memoryObject["amount"].(float64); ok {
					memoryUnit := unitsToTransform["memory"]
					convertedMemoryValue := ConvertMemoryUnit(memoryUnit, memoryValue)
					memoryObject["amount"] = convertedMemoryValue
					if updateUnitsk8s {
						memoryObject["format"] = MemoryUnitk8s[memoryUnit]
					} else {
						memoryObject["format"] = memoryUnit
					}
				}
			}

			cpuObject, ok := sectionObject["cpu"].(map[string]interface{})
			if ok {
				if cpuValue, ok := cpuObject["amount"].(float64); ok {
					cpuUnit := unitsToTransform["cpu"]
					convertedCPUValue := ConvertCPUUnit(cpuUnit, cpuValue)
					cpuObject["amount"] = convertedCPUValue
					if updateUnitsk8s {
						cpuObject["format"] = CPUUnitk8s[cpuUnit]
					} else {
						cpuObject["format"] = cpuUnit
					}
				}
			}
		}
	}

	/*
		Recommendation data is available for three periods
		under cost and performance keys(engines)
		For each of these actual values will be present in
		below mentioned dataBlocks > request and limits
	*/

	// Recommendation section
	recommendation_terms, ok := recommendationJSON["recommendation_terms"].(map[string]interface{})
	if !ok {
		log.Error("recommendation data not found in JSON")
		return recommendationJSON
	}

	for _, period := range RecommendationTerms {
		intervalData, ok := recommendation_terms[period].(map[string]interface{})
		if !ok {
			continue
		}

		/* Hack
		// monitoring_start_time is currently not nullable on DB
		// Hence cannot be set to null while saving response from Kruize
		*/
		// remove nil equivalent monitoring_start_time in API response
		monitoring_start_time := intervalData["monitoring_start_time"]
		if monitoring_start_time == "0001-01-01T00:00:00Z" {
			delete(intervalData, "monitoring_start_time")
		}

		err := truncateDurationInHours(intervalData)
		if !err {
			log.Errorf("error truncating duration_in_hours in term %s\n", period)
		}

		if plotsObject, ok := intervalData["plots"].(map[string]interface{}); ok {
			if plotsDataObject, ok := plotsObject["plots_data"].(map[string]interface{}); ok {
				for _, value := range plotsDataObject {
					if datapointMap, ok := value.(map[string]interface{}); ok {
						if cpuUsage, ok := datapointMap["cpuUsage"].(map[string]interface{}); ok {
							cpuUnit := unitsToTransform["cpu"]
							if _, ok := cpuUsage["format"].(string); ok {
								if updateUnitsk8s {
									cpuUsage["format"] = CPUUnitk8s[cpuUnit]
								} else {
									cpuUsage["format"] = cpuUnit
								}
							}
							for _, key := range []string{"q1", "q3", "min", "max", "median"} {
								cpuValue, _ := cpuUsage[key].(float64)
								cpuUsage[key] = ConvertCPUUnit(cpuUnit, cpuValue)
							}
						}
						if memoryUsage, ok := datapointMap["memoryUsage"].(map[string]interface{}); ok {
							memoryUnit := unitsToTransform["memory"]
							if _, ok := memoryUsage["format"].(string); ok {
								if updateUnitsk8s {
									memoryUsage["format"] = MemoryUnitk8s[memoryUnit]
								} else {
									memoryUsage["format"] = memoryUnit
								}
							}
							for _, key := range []string{"q1", "q3", "min", "max", "median"} {
								memoryValue, _ := memoryUsage[key].(float64)
								memoryUsage[key] = ConvertMemoryUnit(memoryUnit, memoryValue)
							}
						}
					}
				}
			}
		}

		if intervalData["recommendation_engines"] != nil {
			for _, recommendationType := range RecommendationEngines {
				engineData, ok := intervalData["recommendation_engines"].(map[string]interface{})[recommendationType].(map[string]interface{})
				if !ok {
					continue
				}

				for _, dataBlock := range []string{"config", "variation"} {
					recommendationSection, ok := engineData[dataBlock].(map[string]interface{})
					if !ok {
						continue
					}

					for _, section := range []string{"limits", "requests"} {
						sectionObject, ok := recommendationSection[section].(map[string]interface{})
						if ok {
							memoryObject, ok := sectionObject["memory"].(map[string]interface{})
							if ok {
								if memoryValue, ok := memoryObject["amount"].(float64); ok {
									memoryUnit := unitsToTransform["memory"]
									convertedMemoryValue := ConvertMemoryUnit(memoryUnit, memoryValue)
									memoryObject["amount"] = convertedMemoryValue
									if updateUnitsk8s {
										memoryObject["format"] = MemoryUnitk8s[memoryUnit]
									} else {
										memoryObject["format"] = memoryUnit
									}
								}
							}

							cpuObject, ok := sectionObject["cpu"].(map[string]interface{})
							if ok {
								if cpuValue, ok := cpuObject["amount"].(float64); ok {
									cpuUnit := unitsToTransform["cpu"]
									convertedCPUValue := ConvertCPUUnit(cpuUnit, cpuValue)
									cpuObject["amount"] = convertedCPUValue
									if updateUnitsk8s {
										cpuObject["format"] = CPUUnitk8s[cpuUnit]
									} else {
										cpuObject["format"] = cpuUnit
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return recommendationJSON
}

func filterNotifications(recommendationID string, clusterUUID string, recommendationJSON map[string]interface{}) map[string]interface{} {
	var droppedNotifications []string

	deleteNotificationObject := func(recommendationSection map[string]interface{}) {
		notificationObject, ok := recommendationSection["notifications"].(map[string]interface{})
		if ok {
			for key := range notificationObject {
				_, found := NotificationsToShow[key]
				if !found {
					delete(recommendationSection, "notifications")
					droppedNotifications = append(droppedNotifications, key)
				}
			}
		}
	}

	// level 1 notifications are not stored in the database

	// level 2
	deleteNotificationObject(recommendationJSON)

	recommendationTerms, ok := recommendationJSON["recommendation_terms"].(map[string]interface{})
	if !ok {
		log.Error("recommendation data not found in JSON")
		return recommendationJSON
	}

	for _, term := range RecommendationTerms {
		levelThree, ok := recommendationTerms[term].(map[string]interface{})
		if !ok {
			continue
		}
		deleteNotificationObject(levelThree)
		recommendationEngineObject, okEngines := levelThree["recommendation_engines"].(map[string]interface{})
		if !okEngines {
			continue
		}
		for _, engine := range RecommendationEngines {
			levelFour, ok := recommendationEngineObject[engine].(map[string]interface{})
			if ok {
				deleteNotificationObject(levelFour)
			}
		}
	}
	droppedNotificationsString := strings.Join(droppedNotifications, ", ")
	log.Warnf("%s dropped from recommendation ID: %s; cluster ID: %s", droppedNotificationsString, recommendationID, clusterUUID)

	return recommendationJSON
}

func dropBoxPlotsObject(recommendationJSON map[string]interface{}) map[string]interface{} {
	recommendation_terms, ok := recommendationJSON["recommendation_terms"].(map[string]interface{})
	if !ok {
		log.Error("recommendation data not found in JSON")
		return recommendationJSON
	}

	for _, period := range RecommendationTerms {
		intervalData, ok := recommendation_terms[period].(map[string]interface{})
		if !ok {
			continue
		}
		delete(intervalData, "plots")
	}
	return recommendationJSON
}

// convertVariationToPercentage replaces variation amounts in the recommendation JSON with
// percentages relative to the corresponding current amounts. When skipRequests is true the
// "requests" section is left untouched (used when stored *_pct values have already been
// injected via injectStoredRequestVariationPct).
func convertVariationToPercentage(recommendationJSON map[string]interface{}, skipRequests bool) map[string]interface{} {
	var currentCpuLimits, currentMemoryLimits, currentCpuRequests, currentMemoryRequests float64

	current_config, ok := recommendationJSON["current"].(map[string]interface{})
	if !ok {
		log.Error("current not found in JSON")
	}

	for _, section := range []string{"limits", "requests"} {
		sectionObject, ok := current_config[section].(map[string]interface{})
		if ok {
			memoryObject, ok := sectionObject["memory"].(map[string]interface{})
			if ok {
				if memoryValue, ok := memoryObject["amount"].(float64); ok {
					switch section {
					case "limits":
						currentMemoryLimits = memoryValue
					case "requests":
						currentMemoryRequests = memoryValue
					}
				}
			}

			cpuObject, ok := sectionObject["cpu"].(map[string]interface{})
			if ok {
				if cpuValue, ok := cpuObject["amount"].(float64); ok {
					switch section {
					case "limits":
						currentCpuLimits = cpuValue
					case "requests":
						currentCpuRequests = cpuValue
					}
				}
			}
		}
	}

	recommendation_terms, ok := recommendationJSON["recommendation_terms"].(map[string]interface{})
	if !ok {
		log.Error("recommendation data not found in JSON")
		return recommendationJSON
	}

	for _, period := range RecommendationTerms {
		intervalData, ok := recommendation_terms[period].(map[string]interface{})
		if !ok {
			continue
		}

		if intervalData["recommendation_engines"] != nil {
			for _, recommendationType := range RecommendationEngines {
				engineData, ok := intervalData["recommendation_engines"].(map[string]interface{})[recommendationType].(map[string]interface{})
				if !ok {
					continue
				}

				for _, dataBlock := range []string{"variation"} {
					recommendationSection, ok := engineData[dataBlock].(map[string]interface{})
					if !ok {
						continue
					}

					sections := []string{"limits", "requests"}
					if skipRequests {
						sections = []string{"limits"}
					}

					for _, section := range sections {
						sectionObject, ok := recommendationSection[section].(map[string]interface{})
						if ok {
							memoryObject, ok := sectionObject["memory"].(map[string]interface{})
							if ok {
								if memoryValue, ok := memoryObject["amount"].(float64); ok {
									switch section {
									case "limits":
										percentageMemoryValue := utils.CalculatePercentage(memoryValue, currentMemoryLimits)
										memoryObject["amount"] = utils.TruncateToThreeDecimalPlaces(percentageMemoryValue)
									case "requests":
										percentageMemoryValue := utils.CalculatePercentage(memoryValue, currentMemoryRequests)
										memoryObject["amount"] = utils.TruncateToThreeDecimalPlaces(percentageMemoryValue)
									}
									memoryObject["format"] = "percent"
								}
							}

							cpuObject, ok := sectionObject["cpu"].(map[string]interface{})
							if ok {
								if cpuValue, ok := cpuObject["amount"].(float64); ok {
									switch section {
									case "limits":
										percentageCpuValue := utils.CalculatePercentage(cpuValue, currentCpuLimits)
										cpuObject["amount"] = utils.TruncateToThreeDecimalPlaces(percentageCpuValue)
									case "requests":
										percentageCpuValue := utils.CalculatePercentage(cpuValue, currentCpuRequests)
										cpuObject["amount"] = utils.TruncateToThreeDecimalPlaces(percentageCpuValue)
									}
									cpuObject["format"] = "percent"
								}
							}
						}
					}
				}
			}
		}
	}
	return recommendationJSON
}

// injectStoredRequestVariationPct writes the pre-computed *_pct DB column values directly into
// the variation.requests section of the recommendation JSON, replacing the raw amounts. This
// avoids recomputing percentages from the JSON blob for the requests section. The limits section
// is left unchanged and must still be processed by convertVariationToPercentage.
// InjectStoredRequestVariationPct writes pre-computed DB pct values into variation.requests.
func InjectStoredRequestVariationPct(data map[string]interface{}, pcts *StoredVariationPcts) map[string]interface{} {
	terms, ok := data["recommendation_terms"].(map[string]interface{})
	if !ok {
		return data
	}
	for _, spec := range StoredVariationSpecs {
		intervalData, ok := terms[spec.Term].(map[string]interface{})
		if !ok {
			continue
		}
		engines, ok := intervalData["recommendation_engines"].(map[string]interface{})
		if !ok {
			continue
		}
		engine, ok := engines[spec.Engine].(map[string]interface{})
		if !ok {
			continue
		}
		variation, ok := engine["variation"].(map[string]interface{})
		if !ok {
			continue
		}
		requests, ok := variation["requests"].(map[string]interface{})
		if !ok {
			continue
		}
		cpuPct, memPct := spec.CPU(pcts), spec.Mem(pcts)
		if cpu, ok := requests["cpu"].(map[string]interface{}); ok && cpuPct != nil {
			cpu["amount"] = *cpuPct
			cpu["format"] = "percent"
		}
		if mem, ok := requests["memory"].(map[string]interface{}); ok && memPct != nil {
			mem["amount"] = *memPct
			mem["format"] = "percent"
		}
	}
	return data
}

// UpdateRecommendationJSON transforms raw recommendation JSON for API output: unit conversion,
// notification filtering, and variation-to-percentage conversion.
// When storedPcts is provided and has values, the requests variation percentages are taken
// directly from the stored DB columns instead of being recomputed from the JSON blob.
func UpdateRecommendationJSON(handlerName string, recommendationID string, clusterUUID string, unitsToTransform map[string]string, updateUnitsk8s bool, jsonData datatypes.JSON, storedPcts *StoredVariationPcts) map[string]interface{} {
	var data map[string]interface{}
	if len(jsonData) == 0 {
		return map[string]interface{}{}
	}
	err := json.Unmarshal([]byte(jsonData), &data)
	if err != nil {
		log.Error("unable to unmarshall recommendation json")
		return map[string]interface{}{}
	}

	// box-plots data is not required from list endpoints
	if handlerName == "recommendationset-list" || handlerName == "namespace-recommendationset-list" {
		data = dropBoxPlotsObject(data)
	}

	data = transformComponentUnits(unitsToTransform, updateUnitsk8s, data) // cpu: core values require truncation
	data = filterNotifications(recommendationID, clusterUUID, data)

	skipRequests := storedPcts != nil && storedPcts.HasValues()
	if skipRequests {
		data = InjectStoredRequestVariationPct(data, storedPcts)
	}
	data = convertVariationToPercentage(data, skipRequests)
	return data
}
