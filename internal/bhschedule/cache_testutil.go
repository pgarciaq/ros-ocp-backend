package bhschedule

// NewCacheForTest builds an in-memory schedule cache for unit tests.
func NewCacheForTest(org, cluster *Schedule, namespace map[string]Schedule) *Cache {
	c := &Cache{namespace: make(map[string]Schedule)}
	if org != nil {
		s := *org
		_ = initScheduleLocation(&s)
		c.org = &s
	}
	if cluster != nil {
		s := *cluster
		_ = initScheduleLocation(&s)
		c.cluster = &s
	}
	for k, v := range namespace {
		s := v
		_ = initScheduleLocation(&s)
		c.namespace[k] = s
	}
	return c
}
