package kafka

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// panicReader fails the test if ReadMessage is invoked (used when ctx is already cancelled).
type panicReader struct{}

func (panicReader) ReadMessage(time.Duration) (*kafka.Message, error) {
	panic("ReadMessage must not be called when cancelled context exits the loop first")
}

func TestConsumeMessagesUntilCancelled_RespectsImmediateCancellation(t *testing.T) {
	t.Parallel()

	log := logrus.New()
	log.SetOutput(io.Discard)
	entry := logrus.NewEntry(log)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler := func(*kafka.Message, *kafka.Consumer) {
		panic("handler must not run")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		consumeMessagesUntilCancelled(ctx, panicReader{}, nil, handler, entry)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumeMessagesUntilCancelled blocked instead of exiting on cancelled context")
	}
}

func TestConsumerCloseGracePeriod_Configured(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 30*time.Second, ConsumerCloseGracePeriod)
}
