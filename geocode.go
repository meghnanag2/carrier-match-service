package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Geocoder converts a free-text address into coordinates.
// This is the real external dependency in the project: OpenStreetMap's
// public Nominatim API (https://nominatim.org/release-docs/latest/api/Search/).
// It's free and requires no API key, but its usage policy requires:
//   - a descriptive User-Agent identifying the application (not the default
//     Go client UA), and
//   - no more than ~1 request/second from a single client.
// GeocodeAddress below follows both.
type Geocoder struct {
	httpClient *http.Client
	userAgent  string
	cache      *geocodeCache
}

func NewGeocoder() *Geocoder {
	return &Geocoder{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		userAgent:  "carrier-match-service/0.1 (personal project; contact: meghnanag2@gmail.com)",
		cache:      newGeocodeCache(),
	}
}

type nominatimResult struct {
	Lat string `json:"lat"`
	Lon string `json:"lon"`
}

// GeocodeAddress returns (lat, lon) for an address, checking the in-memory
// cache first and only calling Nominatim on a cache miss.
func (g *Geocoder) GeocodeAddress(address string) (lat float64, lon float64, err error) {
	if coords, ok := g.cache.get(address); ok {
		g.cache.recordHit()
		return coords.lat, coords.lon, nil
	}
	g.cache.recordMiss()

	lat, lon, err = g.fetchFromNominatim(address)
	if err != nil {
		return 0, 0, err
	}

	g.cache.set(address, lat, lon)
	return lat, lon, nil
}

// fetchFromNominatim performs the actual outbound HTTP call — split out from
// GeocodeAddress so the caching logic above stays readable on its own.
func (g *Geocoder) fetchFromNominatim(address string) (lat float64, lon float64, err error) {
	endpoint := "https://nominatim.openstreetmap.org/search"

	params := url.Values{}
	params.Set("q", address)
	params.Set("format", "json")
	params.Set("limit", "1")

	req, err := http.NewRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return 0, 0, fmt.Errorf("building geocode request: %w", err)
	}
	// Required by Nominatim's usage policy — requests without a real UA
	// are rate-limited more aggressively or dropped.
	req.Header.Set("User-Agent", g.userAgent)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("calling geocode API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("geocode API returned status %d", resp.StatusCode)
	}

	var results []nominatimResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return 0, 0, fmt.Errorf("decoding geocode response: %w", err)
	}
	if len(results) == 0 {
		return 0, 0, fmt.Errorf("no geocoding match for address %q", address)
	}

	lat, err = strconv.ParseFloat(results[0].Lat, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing latitude: %w", err)
	}
	lon, err = strconv.ParseFloat(results[0].Lon, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing longitude: %w", err)
	}
	return lat, lon, nil
}
