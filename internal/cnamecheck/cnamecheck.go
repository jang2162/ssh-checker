// Package cnamecheck는 공개 리졸버에 _acme-challenge CNAME을 직접 물어 ACME 위임이 걸려 있는지 확인한다.
// 사내 DNS가 아니라 Let's Encrypt가 보는 쪽에 맞추려고 지정한 리졸버에만 묻는다.
package cnamecheck

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"ssl-checker/internal/netutil"
)

type Status string

const (
	StatusUnknown  Status = "unknown" // 아직 확인하지 않음 (DB 기본값)
	StatusOK       Status = "ok"
	StatusMissing  Status = "missing"
	StatusMismatch Status = "mismatch"
	StatusError    Status = "error"
)

const DefaultTimeout = 5 * time.Second

type Result struct {
	Status Status
	Value  string // 조회된 CNAME 값(소문자, 끝 점 없음)
	Err    error  // Status가 error일 때만
}

type Checker struct {
	Resolver string        // host:port
	Timeout  time.Duration // 0이면 DefaultTimeout
}

func ChallengeName(domain string) string { return "_acme-challenge." + domain }

// Check는 domain의 _acme-challenge CNAME이 want를 가리키는지 본다. 대소문자와 끝 점은 무시한다.
func (c *Checker) Check(ctx context.Context, domain, want string) Result {
	value, found, err := c.lookup(ctx, ChallengeName(domain))
	switch {
	case err != nil:
		return Result{Status: StatusError, Err: err}
	case !found:
		return Result{Status: StatusMissing}
	case value == netutil.NormalizeName(want):
		return Result{Status: StatusOK, Value: value}
	default:
		return Result{Status: StatusMismatch, Value: value}
	}
}

// lookup은 CNAME 타입으로 직접 질의한다. net.Resolver.LookupCNAME은 A/AAAA를 묻고 체인을 끝까지
// 따라가므로, TXT만 있는 챌린지 대상에서는 결과가 불분명하다.
func (c *Checker) lookup(ctx context.Context, name string) (string, bool, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	qname, err := dnsmessage.NewName(name + ".")
	if err != nil {
		return "", false, fmt.Errorf("cnamecheck: bad name %q: %w", name, err)
	}
	id := uint16(rand.Uint32())
	q := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: qname, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET}},
	}
	packed, err := q.Pack()
	if err != nil {
		return "", false, fmt.Errorf("cnamecheck: pack: %w", err)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", c.Resolver)
	if err != nil {
		return "", false, fmt.Errorf("cnamecheck: dial %s: %w", c.Resolver, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := conn.Write(packed); err != nil {
		return "", false, fmt.Errorf("cnamecheck: write: %w", err)
	}
	buf := make([]byte, 1232)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return "", false, fmt.Errorf("cnamecheck: read: %w", err)
		}
		var m dnsmessage.Message
		if m.Unpack(buf[:n]) != nil || m.ID != id || !m.Response {
			continue // 다른 질의의 응답이나 깨진 패킷은 무시한다
		}
		switch m.RCode {
		case dnsmessage.RCodeSuccess:
		case dnsmessage.RCodeNameError:
			return "", false, nil
		default:
			return "", false, fmt.Errorf("cnamecheck: %s rcode %s", name, m.RCode)
		}
		for _, a := range m.Answers {
			if a.Header.Type != dnsmessage.TypeCNAME || !strings.EqualFold(a.Header.Name.String(), qname.String()) {
				continue
			}
			if body, ok := a.Body.(*dnsmessage.CNAMEResource); ok {
				return netutil.NormalizeName(body.CNAME.String()), true, nil
			}
		}
		return "", false, nil
	}
}
