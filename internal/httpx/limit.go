package httpx

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// maxKeys를 넘으면 표를 비운다. 사내망 전용 API라 키 수가 적다는 전제의 단순한 상한이다.
const maxKeys = 10000

// Keyed는 키(IP, 계정)별 토큰 버킷이다.
type Keyed struct {
	mu    sync.Mutex
	every rate.Limit
	burst int
	m     map[string]*rate.Limiter
}

func NewKeyed(every time.Duration, burst int) *Keyed {
	return &Keyed{every: rate.Every(every), burst: burst, m: map[string]*rate.Limiter{}}
}

func (k *Keyed) get(key string) *rate.Limiter {
	k.mu.Lock()
	defer k.mu.Unlock()
	l, ok := k.m[key]
	if !ok {
		if len(k.m) >= maxKeys {
			k.m = map[string]*rate.Limiter{}
		}
		l = rate.NewLimiter(k.every, k.burst)
		k.m[key] = l
	}
	return l
}

// Allow는 토큰 하나를 쓴다. 남은 토큰이 없으면 false다.
func (k *Keyed) Allow(key string) bool { return k.get(key).Allow() }

// Exhausted는 토큰을 쓰지 않고 남은 토큰이 없는지만 본다.
func (k *Keyed) Exhausted(key string) bool { return k.get(key).Tokens() < 1 }
