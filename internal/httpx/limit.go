package httpx

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// maxKeys는 표에 담는 키 수의 상한이다. 가득 차면 토큰이 다 찬 버킷(새 버킷과 같은 상태)만 걷어내고,
// 그래도 자리가 없으면 새 키를 거절한다. 소진된 버킷은 절대 지우지 않아 잠금이 풀리지 않는다.
const maxKeys = 10000

// Keyed는 키(IP, 계정)별 토큰 버킷이다.
type Keyed struct {
	mu    sync.Mutex
	every rate.Limit
	burst int
	max   int
	m     map[string]*rate.Limiter
}

func NewKeyed(every time.Duration, burst int) *Keyed {
	return &Keyed{every: rate.Every(every), burst: burst, max: maxKeys, m: map[string]*rate.Limiter{}}
}

// Allow는 토큰 하나를 쓴다. 남은 토큰이 없거나 표가 가득 차 새 키를 받을 수 없으면 false다.
func (k *Keyed) Allow(key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	l, ok := k.m[key]
	if !ok {
		if len(k.m) >= k.max {
			for name, old := range k.m {
				if old.Tokens() >= float64(k.burst) {
					delete(k.m, name)
				}
			}
			if len(k.m) >= k.max {
				return false
			}
		}
		l = rate.NewLimiter(k.every, k.burst)
		k.m[key] = l
	}
	return l.Allow()
}

// Exhausted는 토큰을 쓰지 않고 남은 토큰이 없는지만 본다. 모르는 키는 소진된 것이 아니며 표에 넣지 않는다.
func (k *Keyed) Exhausted(key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	l, ok := k.m[key]
	return ok && l.Tokens() < 1
}
