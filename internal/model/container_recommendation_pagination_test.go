package model

import (
	"strings"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/stretchr/testify/assert"
)

func TestNativeContainerSeekAfter_NilSortValue(t *testing.T) {
	clause, args := nativeContainerSeekAfter(
		"rs.estimated_savings_cents", listoptions.OrderAsc,
		nil,
		"c1", "ns-a", "wl-a", "Deployment", "ctr-a",
	)

	assert.True(t, strings.HasPrefix(clause, "((rs.estimated_savings_cents) IS NULL AND"),
		"nil sort value should restrict to NULL region: got %s", clause)
	assert.Contains(t, clause, "(rs.namespace, rs.workload, rs.workload_type, rs.container_name, rs.cluster_uuid) > (?, ?, ?, ?, ?)")
	assert.Equal(t, []interface{}{"ns-a", "wl-a", "Deployment", "ctr-a", "c1"}, args)
}

func TestNativeContainerSeekAfter_NonNilASC(t *testing.T) {
	clause, args := nativeContainerSeekAfter(
		"rs.estimated_savings_cents", listoptions.OrderAsc,
		int64(500),
		"c1", "ns-a", "wl-a", "Deployment", "ctr-a",
	)

	assert.Contains(t, clause, "(rs.estimated_savings_cents) > ?")
	assert.Contains(t, clause, "(rs.estimated_savings_cents) IS NULL")
	assert.Contains(t, clause, "IS NOT DISTINCT FROM ?")
	assert.Len(t, args, 7)
	assert.Equal(t, int64(500), args[0])
	assert.Equal(t, int64(500), args[1])
}

func TestNativeContainerSeekAfter_NonNilDESC(t *testing.T) {
	clause, args := nativeContainerSeekAfter(
		"rs.estimated_savings_cents", listoptions.OrderDesc,
		int64(1000),
		"c2", "ns-b", "wl-b", "StatefulSet", "ctr-b",
	)

	assert.Contains(t, clause, "(rs.estimated_savings_cents) < ?")
	assert.Contains(t, clause, "(rs.estimated_savings_cents) IS NULL")
	assert.Contains(t, clause, "IS NOT DISTINCT FROM ?")
	assert.Len(t, args, 7)
	assert.Equal(t, int64(1000), args[0])
	assert.Equal(t, int64(1000), args[1])
}

func TestNativeContainerKeysSeekAfter_NilSortValue(t *testing.T) {
	clause, args := nativeContainerKeysSeekAfter(
		"rs.estimated_savings_cents", listoptions.OrderDesc,
		nil,
		"c3", "ns-c", "wl-c", "DaemonSet", "ctr-c",
	)

	assert.True(t, strings.HasPrefix(clause, "((rs.estimated_savings_cents) IS NULL AND"),
		"nil sort value should restrict to NULL region: got %s", clause)
	assert.Contains(t, clause, "(ock.namespace, ock.workload, ock.workload_type, ock.container_name, ock.cluster_uuid) > (?, ?, ?, ?, ?)")
	assert.Equal(t, []interface{}{"ns-c", "wl-c", "DaemonSet", "ctr-c", "c3"}, args)
}

func TestNativeContainerKeysSeekAfter_NonNilASC(t *testing.T) {
	clause, args := nativeContainerKeysSeekAfter(
		"rs.estimated_savings_cents", listoptions.OrderAsc,
		int64(250),
		"c4", "ns-d", "wl-d", "Job", "ctr-d",
	)

	assert.Contains(t, clause, "(rs.estimated_savings_cents) > ?")
	assert.Contains(t, clause, "(rs.estimated_savings_cents) IS NULL")
	assert.Contains(t, clause, "IS NOT DISTINCT FROM ?")
	assert.Len(t, args, 7)
}

func TestNativeContainerDistinctOnOrder_NullsLast(t *testing.T) {
	order := nativeContainerDistinctOnOrder("rs.estimated_savings_cents", "ASC")
	assert.Contains(t, order, "ASC NULLS LAST")

	order = nativeContainerDistinctOnOrder("rs.estimated_savings_cents", "DESC")
	assert.Contains(t, order, "DESC NULLS LAST")
}

func TestNativeContainerKeysPageOrder_NullsLast(t *testing.T) {
	order := nativeContainerKeysPageOrder("rs.estimated_savings_cents", "ASC")
	assert.Contains(t, order, "ASC NULLS LAST")

	order = nativeContainerKeysPageOrder("rs.estimated_savings_cents", "DESC")
	assert.Contains(t, order, "DESC NULLS LAST")
}

func TestNativeContainerPageOrder_NullsLast(t *testing.T) {
	order := nativeContainerPageOrder("page", "ASC")
	assert.Contains(t, order, "ASC NULLS LAST")

	order = nativeContainerPageOrder("page", "DESC")
	assert.Contains(t, order, "DESC NULLS LAST")
}

func TestNativeContainerDetailOrder_NullsLast(t *testing.T) {
	order := nativeContainerDetailOrder("ASC")
	assert.Contains(t, order, "ASC NULLS LAST")

	order = nativeContainerDetailOrder("DESC")
	assert.Contains(t, order, "DESC NULLS LAST")
}
