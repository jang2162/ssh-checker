// Package memory는 TXT를 메모리에만 두는 공급자다. 테스트와 로컬 실행용이며 실제 DNS에는 아무것도 기록하지 않는다.
package memory

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"

	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/netutil"
)

type Provider struct {
	mu      sync.Mutex
	records map[string][]string
	fail    error
}

func New() *Provider { return &Provider{records: map[string][]string{}} }

// SetFailure는 이후 모든 호출이 err를 감싼 일시 오류를 돌려주게 한다. nil이면 해제한다.
func (p *Provider) SetFailure(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail = err
}

// Values는 fqdn의 현재 값을 정렬해 돌려준다. 테스트용.
func (p *Provider) Values(fqdn string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := slices.Clone(p.records[netutil.NormalizeName(fqdn)])
	slices.Sort(out)
	return out
}

func (p *Provider) failure(op string) error {
	if p.fail == nil {
		return nil
	}
	return &dnsprovider.Error{Kind: dnsprovider.KindTransient, Op: op, Err: p.fail}
}

func (p *Provider) SetTXT(_ context.Context, fqdn string, values []string, _ int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.failure("set"); err != nil {
		return err
	}
	fqdn = netutil.NormalizeName(fqdn)
	vals := dnsprovider.Dedupe(values)
	if len(vals) == 0 {
		delete(p.records, fqdn)
		return nil
	}
	p.records[fqdn] = vals
	return nil
}

func (p *Provider) DeleteTXT(_ context.Context, fqdn string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.failure("delete"); err != nil {
		return err
	}
	delete(p.records, netutil.NormalizeName(fqdn))
	return nil
}

func (p *Provider) ListTXT(_ context.Context, zone string) ([]dnsprovider.TXTRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.failure("list"); err != nil {
		return nil, err
	}
	zone = netutil.NormalizeName(zone)
	var out []dnsprovider.TXTRecord
	for fqdn, vals := range p.records {
		if fqdn != zone && !strings.HasSuffix(fqdn, "."+zone) {
			continue
		}
		for _, v := range vals {
			out = append(out, dnsprovider.TXTRecord{FQDN: fqdn, Value: v})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FQDN != out[j].FQDN {
			return out[i].FQDN < out[j].FQDN
		}
		return out[i].Value < out[j].Value
	})
	return out, nil
}

func (p *Provider) Check(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failure("check")
}

func (p *Provider) Capabilities() dnsprovider.Capabilities {
	return dnsprovider.Capabilities{MinTTL: 1}
}
