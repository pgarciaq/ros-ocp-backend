package bhschedule

// NewCacheForTest builds an in-memory schedule cache for unit tests.
func NewCacheForTest(org, cluster *Schedule, namespace map[string]Schedule) *Cache {
	c := &Cache{namespace: make(map[string]Schedule)}
	if org != nil {
		s := *org
		_ = s.InitLocation()
		c.org = &s
	}
	if cluster != nil {
		s := *cluster
		_ = s.InitLocation()
		c.cluster = &s
	}
	for k, v := range namespace {
		s := v
		_ = s.InitLocation()
		c.namespace[k] = s
	}
	return c
}
