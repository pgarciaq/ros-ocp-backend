package bhschedule

// NewCacheForTest builds an in-memory schedule cache for unit tests.
func NewCacheForTest(org, cluster *Schedule, namespace map[string]Schedule) *Cache {
	c := &Cache{namespace: make(map[string]Schedule)}
	if org != nil {
		s := *org
		c.org = &s
	}
	if cluster != nil {
		s := *cluster
		c.cluster = &s
	}
	for k, v := range namespace {
		c.namespace[k] = v
	}
	return c
}
