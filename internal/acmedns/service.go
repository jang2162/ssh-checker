package acmedns

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/netutil"
	"ssl-checker/internal/store"
)

var (
	ErrForbidden     = errors.New("acmedns: forbidden")
	ErrBadTXT        = errors.New("acmedns: bad txt")
	ErrInvalidDomain = errors.New("acmedns: invalid domain")
)

// providerWriteTimeout은 /update 한 번의 공급자 반영 시간 상한이다.
// 계정 행 잠금과 풀 연결을 쥔 채 기다리므로, 핸들러가 서버의 WriteTimeout(30초) 안에 끝나게 한다.
const providerWriteTimeout = 20 * time.Second

// dummyHash는 없는 계정으로 인증할 때도 bcrypt 비교를 해서, 계정 존재 여부가 응답 시간으로 드러나지 않게 한다.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("ssl-checker-dummy-password"), bcrypt.DefaultCost)

type Service struct {
	Store    *store.Store
	Provider dnsprovider.Provider
	Zone     string
	TTL      int
}

// Registration의 Password는 여기서 한 번만 평문으로 나온다. DB에는 bcrypt 해시만 남는다.
type Registration struct {
	Username   string
	Password   string
	Subdomain  string
	FullDomain string
	AllowFrom  []netip.Prefix
}

func (s *Service) FQDN(subdomain string) string { return subdomain + "." + s.Zone }

// Register는 domain의 검증 계정을 만든다. 사용자명과 서브도메인은 무작위 UUID다.
func (s *Service) Register(ctx context.Context, domain string, allowFrom []netip.Prefix) (Registration, error) {
	domain = netutil.NormalizeName(domain)
	if !ValidDomain(domain) {
		return Registration{}, ErrInvalidDomain
	}
	password, hash, err := newKey()
	if err != nil {
		return Registration{}, err
	}
	acct, err := s.Store.CreateAccount(ctx, store.NewAccount{
		Domain:    domain,
		Username:  uuid.New(),
		KeyHash:   hash,
		Subdomain: uuid.New(),
		AllowFrom: allowFrom,
	})
	if err != nil {
		return Registration{}, err
	}
	return Registration{
		Username:   acct.Username,
		Password:   password,
		Subdomain:  acct.Subdomain,
		FullDomain: s.FQDN(acct.Subdomain),
		AllowFrom:  allowFrom,
	}, nil
}

// Authenticate는 X-Api-User, X-Api-Key로 활성 계정을 찾는다. 인증 실패는 이유와 관계없이 ErrForbidden이다.
func (s *Service) Authenticate(ctx context.Context, username, key string) (store.Account, error) {
	acct, err := s.Store.ActiveAccountByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(key))
		return store.Account{}, ErrForbidden
	}
	if err != nil {
		return store.Account{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(acct.KeyHash), []byte(key)) != nil {
		return store.Account{}, ErrForbidden
	}
	return acct, nil
}

// UpdateTXT는 계정을 잠근 채 다음 값 집합을 계산하고, 공급자 반영이 성공했을 때만 DB에 저장한다.
func (s *Service) UpdateTXT(ctx context.Context, acct store.Account, value string) error {
	if !ValidTXT(value) {
		return ErrBadTXT
	}
	fqdn := s.FQDN(acct.Subdomain)
	return s.Store.UpdateTXT(ctx, acct.ID, func(current []string) ([]string, error) {
		next := NextValues(current, value)
		pctx, cancel := context.WithTimeout(ctx, providerWriteTimeout)
		defer cancel()
		if err := s.Provider.SetTXT(pctx, fqdn, next, s.TTL); err != nil {
			return nil, err
		}
		return next, nil
	})
}

// Rotate는 활성 계정의 키를 새로 만든다. 옛 키는 바로 무효가 되고, username과 서브도메인은 그대로다.
func (s *Service) Rotate(ctx context.Context, accountID int64) (Registration, error) {
	password, hash, err := newKey()
	if err != nil {
		return Registration{}, err
	}
	acct, err := s.Store.RotateKey(ctx, accountID, hash)
	if err != nil {
		return Registration{}, err
	}
	return Registration{
		Username:   acct.Username,
		Password:   password,
		Subdomain:  acct.Subdomain,
		FullDomain: s.FQDN(acct.Subdomain),
		AllowFrom:  acct.AllowFrom,
	}, nil
}

// Disable은 계정을 폐기하고 챌린지 TXT를 지운다. 공급자 삭제가 실패하면 계정과 TXT 기록은 그대로 둔다.
func (s *Service) Disable(ctx context.Context, accountID int64) error {
	return s.Store.RevokeAccount(ctx, accountID, func(ctx context.Context, subdomain string) error {
		pctx, cancel := context.WithTimeout(ctx, providerWriteTimeout)
		defer cancel()
		return s.Provider.DeleteTXT(pctx, s.FQDN(subdomain))
	})
}

// newKey는 평문 키와 그 bcrypt 해시다. 평문은 호출한 곳에서 한 번만 내보낸다.
func newKey() (password, hash string, err error) {
	password, err = newPassword()
	if err != nil {
		return "", "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("acmedns: hash key: %w", err)
	}
	return password, string(h), nil
}

// newPassword는 40자 무작위 키를 만든다 (원본 acme-dns와 같은 길이).
func newPassword() (string, error) {
	b := make([]byte, 30)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("acmedns: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
