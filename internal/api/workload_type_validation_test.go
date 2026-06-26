package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateWorkloadTypeValues_AcceptsArbitraryType(t *testing.T) {
	cases := []string{
		"deployment",
		"statefulset",
		"domain",
		"virtualmachine",
		"kafkanodepool",
		"weblogicdomaincontroller",
		"argocdapplication",
		"DEPLOYMENT",
		"DaemonSet",
	}
	for _, tc := range cases {
		assert.NoError(t, validateWorkloadTypeValues([]string{tc}), "expected %q to be accepted", tc)
	}
}

func TestValidateWorkloadTypeValues_AcceptsMultiple(t *testing.T) {
	err := validateWorkloadTypeValues([]string{"deployment", "domain", "virtualmachine"})
	assert.NoError(t, err)
}

func TestValidateWorkloadTypeValues_RejectsEmpty(t *testing.T) {
	err := validateWorkloadTypeValues([]string{""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestValidateWorkloadTypeValues_RejectsWhitespaceOnly(t *testing.T) {
	err := validateWorkloadTypeValues([]string{"   "})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestValidateWorkloadTypeValues_RejectsExceedsMaxLength(t *testing.T) {
	longVal := strings.Repeat("a", 64)
	err := validateWorkloadTypeValues([]string{longVal})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum length")
}

func TestValidateWorkloadTypeValues_AcceptsMaxLength(t *testing.T) {
	maxVal := strings.Repeat("a", 63)
	err := validateWorkloadTypeValues([]string{maxVal})
	assert.NoError(t, err)
}

func TestValidateWorkloadTypeValues_RejectsSentinel(t *testing.T) {
	err := validateWorkloadTypeValues([]string{"<none>"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sentinel")
}

func TestValidateWorkloadTypeValues_RejectsWhitespaceInValue(t *testing.T) {
	err := validateWorkloadTypeValues([]string{"deploy ment"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "whitespace")
}

func TestValidateWorkloadTypeValues_EmptySliceIsValid(t *testing.T) {
	err := validateWorkloadTypeValues([]string{})
	assert.NoError(t, err)

	err = validateWorkloadTypeValues(nil)
	assert.NoError(t, err)
}
