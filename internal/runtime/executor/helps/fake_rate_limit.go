package helps

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

type FakeRateLimitParams struct {
	MaxPercent    int `json:"max-percent"`
	SafeThreshold int `json:"safe-threshold"`
}

type FakeRateLimitAllParams map[string]*FakeRateLimitParams

type fakeRateLimitProvider struct {
	rateLimitMap sync.Map
	maxPct       atomic.Int64
	safeThresh   atomic.Int64
}

func newFakeRateLimitProvider(defaults FakeRateLimitParams) *fakeRateLimitProvider {
	p := &fakeRateLimitProvider{}
	p.maxPct.Store(int64(defaults.MaxPercent))
	p.safeThresh.Store(int64(defaults.SafeThreshold))
	return p
}

func (p *fakeRateLimitProvider) get() FakeRateLimitParams {
	return FakeRateLimitParams{
		MaxPercent:    int(p.maxPct.Load()),
		SafeThreshold: int(p.safeThresh.Load()),
	}
}

func (p *fakeRateLimitProvider) set(params FakeRateLimitParams) {
	if params.MaxPercent >= 0 {
		p.maxPct.Store(int64(params.MaxPercent))
	}
	if params.SafeThreshold >= 0 {
		p.safeThresh.Store(int64(params.SafeThreshold))
	}
	p.rateLimitMap.Clear()
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
	actual, _ := p.rateLimitMap.LoadOrStore(authID, entry)
	val := int(actual.(*atomic.Int64).Load())
	if val < safeThresh {
		return 0
	}
	return val
}

var defaultParams = FakeRateLimitParams{
	MaxPercent:    5,
	SafeThreshold: 5,
}

var fakeRateLimitProviders = map[string]*fakeRateLimitProvider{
	"codex": newFakeRateLimitProvider(defaultParams),
	"xai":   newFakeRateLimitProvider(defaultParams),
}

func GetFakeRateLimitAll() FakeRateLimitAllParams {
	result := make(FakeRateLimitAllParams, len(fakeRateLimitProviders))
	for name, p := range fakeRateLimitProviders {
		result[name] = new(p.get())
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

type FakeRateLimitResponse struct {
	StatusCode int    `json:"status-code"`
	Body       string `json:"body"`
}

type FakeRateLimitResponsesAll map[string][]FakeRateLimitResponse

var fakeRateLimitResponsesMu sync.RWMutex

var fakeRateLimitResponses = FakeRateLimitResponsesAll{
	"codex": {
		{429, `{"error":{"type":"invalid_request_error","code":"rate_limit_exceeded","message":"You've exceeded the rate limit, please slow down and try again after 60.{{RAND6}} seconds.","param":null}}`},
		{429, `{"error":{"type":"rate_limit_error","code": "slow_down","message": "Please slow down and try again later.","param":null}}`},
		{503, `{"error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later.","param":null}}`},
	},
	"xai": {
		{429, `{"code":"resource-exhausted","error":"Too many requests. Your team's rate limit has been exceeded."}`},
	},
}

func GetFakeRateLimitResponses() FakeRateLimitResponsesAll {
	fakeRateLimitResponsesMu.RLock()
	defer fakeRateLimitResponsesMu.RUnlock()
	result := make(FakeRateLimitResponsesAll, len(fakeRateLimitResponses))
	for name, responses := range fakeRateLimitResponses {
		copied := make([]FakeRateLimitResponse, len(responses))
		copy(copied, responses)
		result[name] = copied
	}
	return result
}

func SetFakeRateLimitResponses(all FakeRateLimitResponsesAll) {
	fakeRateLimitResponsesMu.Lock()
	defer fakeRateLimitResponsesMu.Unlock()
	for name, responses := range all {
		if len(responses) == 0 {
			continue
		}
		fakeRateLimitResponses[name] = responses
	}
}

func CheckFakeRateLimit(ctx context.Context, cfg *config.Config, provider, authID string) (int, []byte, bool) {
	if rand.Intn(100) >= FakeRateLimitFor(provider, authID) {
		return 0, nil, false
	}
	fakeRateLimitResponsesMu.RLock()
	responses := fakeRateLimitResponses[provider]
	r := responses[rand.Intn(len(responses))]
	fakeRateLimitResponsesMu.RUnlock()
	body := r.Body
	if strings.Contains(body, "{{RAND6}}") {
		body = strings.Replace(body, "{{RAND6}}", fmt.Sprintf("%06d", rand.Intn(1000000)), 1)
	}
	RecordAPIResponseMetadata(ctx, cfg, r.StatusCode, http.Header{"Content-Type": []string{"application/json"}})
	return r.StatusCode, []byte(body), true
}
