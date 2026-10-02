package acmedns

import (
	"context"
	"sync"
	"time"

	"ssl-checker/internal/dnsprovider"
)

// 공급자 확인은 호출한 /health 요청의 취소와 무관하게 자체 제한 시간을 둔다.
// 요청이 먼저 끊겨도 context.Canceled가 캐시에 남아 정상 공급자가 한동안 transient로 보이는 일을 막는다.
const providerCheckTimeout = 3 * time.Second

type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthChecker는 DB 연결과 DNS 공급자 Check를 확인한다.
// 공급자 확인은 외부 API를 부르므로 CacheFor 동안 결과를 재사용한다.
type HealthChecker struct {
	DB       Pinger
	Provider dnsprovider.Provider
	CacheFor time.Duration

	mu      sync.Mutex
	checked time.Time
	provErr error
}

type HealthStatus struct {
	DB       string `json:"db"`
	Provider string `json:"provider"`
}

func (h *HealthChecker) Check(ctx context.Context) (HealthStatus, bool) {
	st := HealthStatus{DB: "ok", Provider: "ok"}
	healthy := true
	if err := h.DB.Ping(ctx); err != nil {
		st.DB = "error"
		healthy = false
	}
	if err := h.providerErr(ctx); err != nil {
		st.Provider = dnsprovider.KindOf(err).String()
		healthy = false
	}
	return st, healthy
}

func (h *HealthChecker) providerErr(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.checked.IsZero() && time.Since(h.checked) < h.CacheFor {
		return h.provErr
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerCheckTimeout)
	defer cancel()
	h.provErr = h.Provider.Check(cctx)
	h.checked = time.Now()
	return h.provErr
}
