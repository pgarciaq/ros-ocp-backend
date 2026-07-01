package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppendUniqueInt16Slice(t *testing.T) {
	tests := []struct {
		name     string
		codes    []int16
		code     int16
		wantLen  int
	}{
		{"append to nil", nil, 42, 1},
		{"append new", []int16{1, 2}, 3, 3},
		{"already present", []int16{1, 2, 3}, 2, 3},
		{"append to empty", []int16{}, 5, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendUniqueInt16Slice(tt.codes, tt.code)
			assert.Len(t, got, tt.wantLen)
		})
	}
}
