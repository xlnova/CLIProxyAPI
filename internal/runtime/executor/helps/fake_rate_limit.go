package helps

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type FakeRateLimitParams struct {
	RefreshMinMinutes int `json:"refresh-min-minutes"`
	RefreshMaxMinutes int `json:"refresh-max-minutes"`
	MaxPercent        int `json:"max-percent"`
	SafeThreshold     int `json:"safe-threshold"`
}

type FakeRateLimitAllParams map[string]*FakeRateLimitParams

type fakeRateLimitProvider struct {
	rateLimitMap sync.Map
	refreshMin   atomic.Int64
	refreshMax   atomic.Int64
	maxPct       atomic.Int64
	safeThresh   atomic.Int64
}

func newFakeRateLimitProvider(defaults FakeRateLimitParams) *fakeRateLimitProvider {
	p := &fakeRateLimitProvider{}
	p.refreshMin.Store(int64(defaults.RefreshMinMinutes))
	p.refreshMax.Store(int64(defaults.RefreshMaxMinutes))
	p.maxPct.Store(int64(defaults.MaxPercent))
	p.safeThresh.Store(int64(defaults.SafeThreshold))
	return p
}

func (p *fakeRateLimitProvider) get() FakeRateLimitParams {
	return FakeRateLimitParams{
		RefreshMinMinutes: int(p.refreshMin.Load()),
		RefreshMaxMinutes: int(p.refreshMax.Load()),
		MaxPercent:        int(p.maxPct.Load()),
		SafeThreshold:     int(p.safeThresh.Load()),
	}
}

func (p *fakeRateLimitProvider) set(params FakeRateLimitParams) {
	if params.RefreshMinMinutes > 0 {
		p.refreshMin.Store(int64(params.RefreshMinMinutes))
	}
	if params.RefreshMaxMinutes > 0 {
		p.refreshMax.Store(int64(params.RefreshMaxMinutes))
	}
	if params.MaxPercent >= 0 {
		p.maxPct.Store(int64(params.MaxPercent))
	}
	if params.SafeThreshold >= 0 {
		p.safeThresh.Store(int64(params.SafeThreshold))
	}
}

func (p *fakeRateLimitProvider) forAuth(authID string) int {
	maxPct := int(p.maxPct.Load())
	safeThresh := int(p.safeThresh.Load())
	if maxPct <= 0 {
		return 0
	}
	if v, ok := p.rateLimitMap.Load(authID); ok {
		val := int(v.(*atomic.Int64).Load())
		if val < safeThresh {
			return 0
		}
		return val
	}
	entry := &atomic.Int64{}
	entry.Store(int64(rand.Intn(maxPct + 1)))
	actual, loaded := p.rateLimitMap.LoadOrStore(authID, entry)
	a := actual.(*atomic.Int64)
	if !loaded {
		go func() {
			for {
				rMin := int(p.refreshMin.Load())
				rMax := int(p.refreshMax.Load())
				rng := rMax - rMin + 1
				if rng <= 0 {
					rng = 1
				}
				time.Sleep(time.Duration(rMin+rand.Intn(rng)) * time.Minute)
				mp := int(p.maxPct.Load())
				if mp <= 0 {
					a.Store(0)
				} else {
					a.Store(int64(rand.Intn(mp + 1)))
				}
			}
		}()
	}
	val := int(a.Load())
	if val < safeThresh {
		return 0
	}
	return val
}

var defaultParams = FakeRateLimitParams{
	RefreshMinMinutes: 10,
	RefreshMaxMinutes: 20,
	MaxPercent:        5,
	SafeThreshold:     5,
}

var fakeRateLimitProviders = map[string]*fakeRateLimitProvider{
	"codex": newFakeRateLimitProvider(defaultParams),
	"xai":   newFakeRateLimitProvider(defaultParams),
}

func GetFakeRateLimitAll() FakeRateLimitAllParams {
	result := make(FakeRateLimitAllParams, len(fakeRateLimitProviders))
	for name, p := range fakeRateLimitProviders {
		params := p.get()
		result[name] = &params
	}
	return result
}

func SetFakeRateLimitAll(all FakeRateLimitAllParams) {
	for name, params := range all {
		if params == nil {
			continue
		}
		p, ok := fakeRateLimitProviders[name]
		if !ok {
			continue
		}
		p.set(*params)
	}
}

func FakeRateLimitFor(provider, authID string) int {
	p, ok := fakeRateLimitProviders[provider]
	if !ok {
		return 0
	}
	return p.forAuth(authID)
}
