package kafka

import (
	"sync"
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCommitter records CommitMessage calls (the MessageCommitter interface
// exists so commit behavior is testable without a *kafka.Consumer).
type fakeCommitter struct {
	mu   sync.Mutex
	msgs []*kafka.Message
	err  error
}

func (f *fakeCommitter) CommitMessage(m *kafka.Message) ([]kafka.TopicPartition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, m)
	return nil, f.err
}

// #756: with partition-ordered queues, commits within a partition are serial
// by construction (the handler tail and panic recovery both run in the
// partition's goroutine), so CommitMessage no longer serializes with a global
// mutex. Cross-partition commits are covered by librdkafka's documented
// thread-safety ("The API is completely thread-safe" — librdkafka
// INTRODUCTION.md, Threads and callbacks). CommitMessage is a nil-safe
// passthrough.
func TestCommitMessage_NilConsumerAndMessageAreNoOps(t *testing.T) {
	assert.NoError(t, CommitMessage(nil, &kafka.Message{}))
	assert.NoError(t, CommitMessage(nil, nil))

	f := &fakeCommitter{}
	assert.NoError(t, CommitMessage(f, nil))
	assert.Empty(t, f.msgs)
}

func TestCommitMessage_PassesThroughToCommitter(t *testing.T) {
	f := &fakeCommitter{}
	msg := &kafka.Message{}

	require.NoError(t, CommitMessage(f, msg))
	require.Equal(t, []*kafka.Message{msg}, f.msgs)

	f.err = assert.AnError
	assert.ErrorIs(t, CommitMessage(f, msg), assert.AnError)
}
