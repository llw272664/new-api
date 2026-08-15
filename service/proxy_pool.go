package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

// --- Proxy Pool Settings ---

type ProxyPoolSettings struct {
	Enabled                 bool   `json:"enabled"`
	DefaultGroup            string `json:"default_group"`
	Strategy                string `json:"strategy"`                  // weighted_round_robin, round_robin, random
	CircuitBreakerThreshold int    `json:"circuit_breaker_threshold"` // failures before disabling
	HealthCheckInterval     int    `json:"health_check_interval"`     // seconds
	MaxConcurrentPerProxy   int    `json:"max_concurrent_per_proxy"`
}

var proxyPoolSettings = ProxyPoolSettings{
	Enabled:                 false,
	DefaultGroup:            "default",
	Strategy:                "weighted_round_robin",
	CircuitBreakerThreshold: 5,
	HealthCheckInterval:     60,
	MaxConcurrentPerProxy:   10,
}

var proxyPoolSettingsMutex sync.RWMutex

const proxyPoolSettingsOptionKey = "ProxyPoolSettings"

func GetProxyPoolSettings() ProxyPoolSettings {
	proxyPoolSettingsMutex.RLock()
	defer proxyPoolSettingsMutex.RUnlock()
	return proxyPoolSettings
}

func LoadProxyPoolSettingsFromDB() {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[proxyPoolSettingsOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if raw == "" {
		return
	}
	var settings ProxyPoolSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		common.SysError("failed to load proxy pool settings: " + err.Error())
		return
	}
	proxyPoolSettingsMutex.Lock()
	proxyPoolSettings = settings
	proxyPoolSettingsMutex.Unlock()
}

func SetProxyPoolSettings(settings ProxyPoolSettings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if err = model.UpdateOption(proxyPoolSettingsOptionKey, string(data)); err != nil {
		return err
	}
	common.OptionMapRWMutex.Lock()
	common.OptionMap[proxyPoolSettingsOptionKey] = string(data)
	common.OptionMapRWMutex.Unlock()
	proxyPoolSettingsMutex.Lock()
	proxyPoolSettings = settings
	proxyPoolSettingsMutex.Unlock()
	return nil
}

// --- Circuit Breaker ---

type circuitState struct {
	failures  int32
	successes int32
	open      bool
	openedAt  time.Time
}

var circuitBreakers = struct {
	sync.RWMutex
	states map[uint]*circuitState
}{
	states: make(map[uint]*circuitState),
}

func getCircuitState(id uint) *circuitState {
	circuitBreakers.Lock()
	defer circuitBreakers.Unlock()
	cs, ok := circuitBreakers.states[id]
	if !ok {
		cs = &circuitState{}
		circuitBreakers.states[id] = cs
	}
	return cs
}

func circuitBreakRecordSuccess(id uint) {
	cs := getCircuitState(id)
	atomic.StoreInt32(&cs.failures, 0)
	atomic.AddInt32(&cs.successes, 1)
	circuitBreakers.Lock()
	cs.open = false
	circuitBreakers.Unlock()
}

func circuitBreakRecordFailure(id uint) {
	cs := getCircuitState(id)
	failures := atomic.AddInt32(&cs.failures, 1)
	atomic.StoreInt32(&cs.successes, 0)
	threshold := int32(proxyPoolSettings.CircuitBreakerThreshold)
	if threshold <= 0 {
		threshold = 5
	}
	if failures >= threshold {
		circuitBreakers.Lock()
		cs.open = true
		cs.openedAt = time.Now()
		circuitBreakers.Unlock()
		logger.LogWarn(context.Background(), fmt.Sprintf("proxy pool circuit breaker OPEN for proxy %d after %d failures", id, failures))
	}
}

func isCircuitOpen(id uint) bool {
	cs := getCircuitState(id)
	if atomic.LoadInt32(&cs.failures) == 0 {
		return false
	}
	circuitBreakers.RLock()
	defer circuitBreakers.RUnlock()
	if !cs.open {
		return false
	}
	// Half-open after 60 seconds
	if time.Since(cs.openedAt) > 60*time.Second {
		return false
	}
	return true
}

// --- Concurrency Limiter ---

var concurrencyLimiters = struct {
	sync.Mutex
	limits map[string]*int64
}{
	limits: make(map[string]*int64),
}

func tryAcquireProxySlot(proxyKey string) bool {
	concurrencyLimiters.Lock()
	counter, ok := concurrencyLimiters.limits[proxyKey]
	if !ok {
		var c int64
		counter = &c
		concurrencyLimiters.limits[proxyKey] = counter
	}
	concurrencyLimiters.Unlock()

	max := int64(proxyPoolSettings.MaxConcurrentPerProxy)
	if max <= 0 {
		max = 10
	}
	for {
		current := atomic.LoadInt64(counter)
		if current >= max {
			return false
		}
		if atomic.CompareAndSwapInt64(counter, current, current+1) {
			return true
		}
	}
}

func releaseProxySlot(proxyKey string) {
	concurrencyLimiters.Lock()
	counter, ok := concurrencyLimiters.limits[proxyKey]
	concurrencyLimiters.Unlock()
	if ok {
		atomic.AddInt64(counter, -1)
	}
}

// --- Proxy Selection (Weighted Round Robin) ---

// SelectProxyFromPool picks the next proxy from a group using weighted round-robin.
func SelectProxyFromPool(group string) (*model.ProxyPoolEntry, error) {
	settings := GetProxyPoolSettings()
	if !settings.Enabled {
		return nil, fmt.Errorf("proxy pool is disabled")
	}
	if group == "" {
		group = settings.DefaultGroup
	}

	entries := model.GetProxyPoolEntriesByGroup(group)
	if len(entries) == 0 {
		return nil, fmt.Errorf("no proxies in group %q", group)
	}

	// Filter out circuit-broken proxies
	available := make([]*model.ProxyPoolEntry, 0, len(entries))
	for _, e := range entries {
		if !isCircuitOpen(e.ID) {
			available = append(available, e)
		}
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("all proxies in group %q are circuit-broken", group)
	}

	switch settings.Strategy {
	case "round_robin":
		return selectRoundRobin(available, group), nil
	case "random":
		idx := time.Now().UnixNano() % int64(len(available))
		return available[idx], nil
	default: // weighted_round_robin
		return selectWeightedRoundRobin(available, group), nil
	}
}

func selectRoundRobin(entries []*model.ProxyPoolEntry, group string) *model.ProxyPoolEntry {
	idx := model.NextProxyPoolRRIndex(group)
	return entries[int(idx%uint(len(entries)))]
}

func selectWeightedRoundRobin(entries []*model.ProxyPoolEntry, group string) *model.ProxyPoolEntry {
	totalWeight := 0
	for _, e := range entries {
		w := e.Weight
		if w <= 0 {
			w = 1
		}
		totalWeight += w
	}

	idx := model.NextProxyPoolRRIndex(group)

	target := int(idx) % totalWeight
	cumulative := 0
	for _, e := range entries {
		w := e.Weight
		if w <= 0 {
			w = 1
		}
		cumulative += w
		if target < cumulative {
			return e
		}
	}
	return entries[0]
}

// GetProxyClientForRelay returns the selected HTTP client and a release callback.
// The caller must invoke the callback after the response body is consumed or closed.
func GetProxyClientForRelay(channelProxy string, poolGroup string) (*http.Client, func(), error) {
	if channelProxy != "" {
		client, err := GetHttpClientWithProxy(channelProxy)
		return client, nil, err
	}

	settings := GetProxyPoolSettings()
	if settings.Enabled {
		for attempt := 0; attempt < 4; attempt++ {
			entry, err := SelectProxyFromPool(poolGroup)
			if err != nil || entry == nil {
				break
			}
			proxyKey := fmt.Sprintf("pool_%d", entry.ID)
			if !tryAcquireProxySlot(proxyKey) {
				continue
			}
			proxyURL, err := entry.ToProxyURL()
			if err != nil || proxyURL == "" {
				releaseProxySlot(proxyKey)
				continue
			}
			client, err := GetHttpClientWithProxy(proxyURL)
			if err != nil {
				releaseProxySlot(proxyKey)
				continue
			}
			model.RecordProxyPoolEntryUsed(entry.ID)
			var releaseOnce sync.Once
			return client, func() {
				releaseOnce.Do(func() { ReleaseProxySlot(entry.ID) })
			}, nil
		}
	}

	return GetHttpClient(), nil, nil
}

// ReleaseProxySlot releases a concurrency slot for a proxy.
func ReleaseProxySlot(entryID uint) {
	proxyKey := fmt.Sprintf("pool_%d", entryID)
	releaseProxySlot(proxyKey)
}

// --- Health Check ---

// CheckProxyHealth tests a single proxy by making a request through it.
func CheckProxyHealth(entry *model.ProxyPoolEntry) bool {
	settings := GetProxyPoolSettings()
	proxyURL, err := entry.ToProxyURL()
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("proxy pool health check: cannot build URL for %s: %v", entry.Name, err))
		return false
	}

	client, err := GetHttpClientWithProxy(proxyURL)
	if err != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.google.com/generate_204", nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		circuitBreakRecordFailure(entry.ID)
		newFailCount := entry.FailCount + 1
		if newFailCount > settings.CircuitBreakerThreshold {
			model.UpdateProxyPoolEntryStatus(entry.ID, "unhealthy", newFailCount, entry.SuccessCount)
		} else {
			model.UpdateProxyPoolEntryStatus(entry.ID, "unknown", newFailCount, entry.SuccessCount)
		}
		return false
	}
	defer resp.Body.Close()

	circuitBreakRecordSuccess(entry.ID)
	newSuccessCount := entry.SuccessCount + 1
	model.UpdateProxyPoolEntryStatus(entry.ID, "healthy", 0, newSuccessCount)
	return true
}

// RunHealthCheckLoop runs periodic health checks for all proxy pool entries.
func RunHealthCheckLoop() {
	settings := GetProxyPoolSettings()
	if !settings.Enabled {
		return
	}

	go func() {
		interval := time.Duration(settings.HealthCheckInterval) * time.Second
		if interval < 10*time.Second {
			interval = 10 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			if !GetProxyPoolSettings().Enabled {
				continue
			}
			entries := model.LoadProxyPoolCache()
			for _, entry := range entries {
				if entry.Enabled {
					go CheckProxyHealth(entry)
				}
			}
		}
	}()
}

// --- Record Success/Failure for relay requests ---

func RecordProxyPoolRequestSuccess(entryID uint) {
	circuitBreakRecordSuccess(entryID)
}

func RecordProxyPoolRequestFailure(entryID uint) {
	circuitBreakRecordFailure(entryID)
}
