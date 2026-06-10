package housekeeper

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWaitHousekeeperShutdownGrace(t *testing.T) {
	start := time.Now()
	waitHousekeeperShutdownGrace(1)
	assert.GreaterOrEqual(t, time.Since(start), time.Second)
}

func TestDeletePartitions_RespectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	DeletePartitions(ctx)
	assert.Less(t, time.Since(start), 2*time.Second)
}
