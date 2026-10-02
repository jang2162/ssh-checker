package acmedns

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"

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
	password, err := newPassword()
	if err != nil {
		return Registration{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Registration{}, fmt.Errorf("acmedns: hash key: %w", err)
	}
	acct, err := s.Store.CreateAccount(ctx, store.NewAccount{
		Domain:    domain,
		Username:  uuid.New(),
		KeyHash:   string(hash),
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
		if err := s.Provider.SetTXT(ctx, fqdn, next, s.TTL); err != nil {
			return nil, err
		}
		return next, nil
	})
}

// newPassword는 40자 무작위 키를 만든다 (원본 acme-dns와 같은 길이).
func newPassword() (string, error) {
	b := make([]byte, 30)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("acmedns: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
