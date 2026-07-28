package main

import "sync"

// geocodeCache caches address -> coordinates lookups.
//
// Why this exists: Nominatim's usage policy caps requests at roughly
// 1/second per client. If the same address gets registered or looked up
// more than once (which will happen — a carrier's address doesn't change
// between requests), re-geocoding it every time is both wasteful and risks
// tripping that rate limit. This is a real, specific reason to cache here —
// not caching "because production systems have caching."
//
// Deliberately in-memory + mutex-guarded, matching store.go's pattern,
// rather than reaching for Redis: the cached data (address -> lat/lon) is
// immutable and small, so a distributed cache wouldn't buy anything a
// process-local one doesn't already give — the honest reason to add Redis
// here would be sharing the cache across multiple running instances of this
// service, not raw lookup speed.
type geocodeCache struct {
	mu    sync.RWMutex
	cache map[string]coordinates
	hits  int
	misses int
}

type coordinates struct {
	lat float64
	lon float64
}

func newGeocodeCache() *geocodeCache {
	return &geocodeCache{cache: make(map[string]coordinates)}
}

func (c *geocodeCache) get(address string) (coordinates, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	coords, ok := c.cache[address]
	return coords, ok
}

func (c *geocodeCache) set(address string, lat, lon float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[address] = coordinates{lat: lat, lon: lon}
}

// Stats returns cache hit/miss counts — exposed for the /health endpoint so
// it's actually observable whether caching is doing anything, rather than
// just trusting that it is.
func (c *geocodeCache) Stats() (hits, misses int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits, c.misses
}

func (c *geocodeCache) recordHit() {
	c.mu.Lock()
	c.hits++
	c.mu.Unlock()
}

func (c *geocodeCache) recordMiss() {
	c.mu.Lock()
	c.misses++
	c.mu.Unlock()
}
