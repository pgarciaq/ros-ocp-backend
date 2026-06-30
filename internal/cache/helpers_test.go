package cache

import (
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveByPrefix_RemovesMatchingKeys(t *testing.T) {
	c := expirable.NewLRU[string, int](100, nil, time.Minute)
	c.Add("org1:a", 1)
	c.Add("org1:b", 2)
	c.Add("org2:a", 3)
	c.Add("org2:b", 4)

	RemoveByPrefix(c, "org1:")
	assert.Equal(t, 2, c.Len())

	_, ok := c.Get("org1:a")
	assert.False(t, ok)
	_, ok = c.Get("org1:b")
	assert.False(t, ok)
	v, ok := c.Get("org2:a")
	require.True(t, ok)
	assert.Equal(t, 3, v)
}

func TestRemoveByPrefix_NoMatchDoesNothing(t *testing.T) {
	c := expirable.NewLRU[string, int](100, nil, time.Minute)
	c.Add("org1:a", 1)
	c.Add("org1:b", 2)

	RemoveByPrefix(c, "org9:")
	assert.Equal(t, 2, c.Len())
}

func TestRemoveByPrefix_EmptyCache(t *testing.T) {
	c := expirable.NewLRU[string, int](100, nil, time.Minute)
	RemoveByPrefix(c, "anything:")
	assert.Equal(t, 0, c.Len())
}

func TestRemoveByPrefix_EmptyPrefix(t *testing.T) {
	c := expirable.NewLRU[string, int](100, nil, time.Minute)
	c.Add("a", 1)
	c.Add("b", 2)

	RemoveByPrefix(c, "")
	assert.Equal(t, 0, c.Len(), "empty prefix matches everything")
}
