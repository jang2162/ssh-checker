// Package cloudflare는 Cloudflare DNS API로 챌린지 TXT를 기록하는 공급자 구현체다.
// Cloudflare는 레코드 단위 API라서, SetTXT는 현재 레코드와 원하는 값 집합을 비교해 생성·교체·삭제한다.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/netutil"
)

const (
	DefaultBaseURL = "https://api.cloudflare.com/client/v4"
	minTTL         = 60
	perPage        = 100
	maxBodyBytes   = 4 << 20
)

type Config struct {
	Token      string
	Zone       string // 챌린지 전용 존 이름 (예: acme-chk.net)
	ZoneID     string // 비어 있으면 Zone 이름으로 조회한다
	BaseURL    string
	HTTPClient *http.Client
}

type Provider struct {
	cfg Config
	hc  *http.Client

	mu     sync.Mutex
	zoneID string
}

func New(cfg Config) *Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	cfg.Zone = netutil.NormalizeName(cfg.Zone)
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Provider{cfg: cfg, hc: hc, zoneID: cfg.ZoneID}
}

type record struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	Success    bool            `json:"success"`
	Errors     []apiError      `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo *struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

// httpError는 Cloudflare가 실패로 응답한 경우다. 토큰은 담지 않는다.
type httpError struct {
	Status int
	Errors []apiError
}

func (e *httpError) Error() string {
	msgs := make([]string, 0, len(e.Errors))
	for _, a := range e.Errors {
		msgs = append(msgs, fmt.Sprintf("%d %s", a.Code, a.Message))
	}
	return fmt.Sprintf("cloudflare: HTTP %d: %s", e.Status, strings.Join(msgs, "; "))
}

func (p *Provider) Capabilities() dnsprovider.Capabilities {
	return dnsprovider.Capabilities{MinTTL: minTTL}
}

func (p *Provider) Check(ctx context.Context) error {
	zid, err := p.zone(ctx)
	if err != nil {
		return err
	}
	q := url.Values{"type": {"TXT"}, "per_page": {"5"}}
	_, err = p.do(ctx, "check", http.MethodGet, "/zones/"+zid+"/dns_records", q, nil, nil)
	return err
}

func (p *Provider) SetTXT(ctx context.Context, fqdn string, values []string, ttl int) error {
	fqdn = netutil.NormalizeName(fqdn)
	ttl = max(ttl, minTTL)
	desired := dnsprovider.Dedupe(values)
	zid, err := p.zone(ctx)
	if err != nil {
		return err
	}
	current, err := p.list(ctx, zid, fqdn)
	if err != nil {
		return err
	}
	kept := map[string]bool{}
	var spare []record
	for _, r := range current {
		v := unquote(r.Content)
		if slices.Contains(desired, v) && !kept[v] {
			kept[v] = true
			continue
		}
		spare = append(spare, r)
	}
	for _, v := range desired {
		if kept[v] {
			continue
		}
		body := record{Type: "TXT", Name: fqdn, Content: v, TTL: ttl}
		if len(spare) > 0 {
			r := spare[0]
			spare = spare[1:]
			if _, err := p.do(ctx, "set", http.MethodPut, "/zones/"+zid+"/dns_records/"+r.ID, nil, body, nil); err != nil {
				return err
			}
			continue
		}
		if _, err := p.do(ctx, "set", http.MethodPost, "/zones/"+zid+"/dns_records", nil, body, nil); err != nil {
			return err
		}
	}
	for _, r := range spare {
		if err := p.deleteRecord(ctx, zid, r.ID); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) DeleteTXT(ctx context.Context, fqdn string) error {
	zid, err := p.zone(ctx)
	if err != nil {
		return err
	}
	current, err := p.list(ctx, zid, netutil.NormalizeName(fqdn))
	if err != nil {
		return err
	}
	for _, r := range current {
		if err := p.deleteRecord(ctx, zid, r.ID); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) ListTXT(ctx context.Context, zone string) ([]dnsprovider.TXTRecord, error) {
	zone = netutil.NormalizeName(zone)
	zid, err := p.zone(ctx)
	if err != nil {
		return nil, err
	}
	recs, err := p.list(ctx, zid, "")
	if err != nil {
		return nil, err
	}
	out := make([]dnsprovider.TXTRecord, 0, len(recs))
	for _, r := range recs {
		name := netutil.NormalizeName(r.Name)
		if name != zone && !strings.HasSuffix(name, "."+zone) {
			continue
		}
		out = append(out, dnsprovider.TXTRecord{FQDN: name, Value: unquote(r.Content), ProviderRef: r.ID})
	}
	return out, nil
}

func (p *Provider) deleteRecord(ctx context.Context, zid, id string) error {
	_, err := p.do(ctx, "delete", http.MethodDelete, "/zones/"+zid+"/dns_records/"+id, nil, nil, nil)
	var he *httpError
	if errors.As(err, &he) && he.Status == http.StatusNotFound {
		return nil // 이미 지워졌다
	}
	return err
}

// zone은 존 ID를 돌려준다. 설정에 없으면 이름으로 한 번 조회해 기억한다.
func (p *Provider) zone(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.zoneID != "" {
		return p.zoneID, nil
	}
	var zones []struct {
		ID string `json:"id"`
	}
	if _, err := p.do(ctx, "zone", http.MethodGet, "/zones", url.Values{"name": {p.cfg.Zone}}, nil, &zones); err != nil {
		return "", err
	}
	if len(zones) == 0 {
		return "", &dnsprovider.Error{Kind: dnsprovider.KindConfig, Op: "zone",
			Err: fmt.Errorf("cloudflare: zone %q not found or token cannot read it (set CF_ZONE_ID)", p.cfg.Zone)}
	}
	p.zoneID = zones[0].ID
	return p.zoneID, nil
}

// list는 TXT 레코드를 모든 페이지에 걸쳐 읽는다. name이 비면 존 전체다.
func (p *Provider) list(ctx context.Context, zid, name string) ([]record, error) {
	var all []record
	for page := 1; ; page++ {
		q := url.Values{"type": {"TXT"}, "per_page": {strconv.Itoa(perPage)}, "page": {strconv.Itoa(page)}}
		if name != "" {
			q.Set("name", name)
		}
		var recs []record
		env, err := p.do(ctx, "list", http.MethodGet, "/zones/"+zid+"/dns_records", q, nil, &recs)
		if err != nil {
			return nil, err
		}
		for _, r := range recs {
			// name 필터가 무시되더라도 다른 이름의 레코드를 건드리지 않도록 직접 거른다.
			if name != "" && netutil.NormalizeName(r.Name) != name {
				continue
			}
			all = append(all, r)
		}
		if env.ResultInfo == nil || page >= env.ResultInfo.TotalPages {
			return all, nil
		}
	}
}

func (p *Provider) do(ctx context.Context, op, method, path string, q url.Values, body, out any) (*envelope, error) {
	fail := func(kind dnsprovider.ErrorKind, err error) (*envelope, error) {
		return nil, &dnsprovider.Error{Kind: kind, Op: op, Err: err}
	}
	u := p.cfg.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fail(dnsprovider.KindConfig, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return fail(dnsprovider.KindConfig, err)
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return fail(dnsprovider.KindTransient, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return fail(dnsprovider.KindTransient, err)
	}
	var env envelope
	decErr := json.Unmarshal(raw, &env)
	if resp.StatusCode >= 300 || decErr != nil || !env.Success {
		kind := dnsprovider.KindConfig
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 || resp.StatusCode < 300 {
			kind = dnsprovider.KindTransient
		}
		return fail(kind, &httpError{Status: resp.StatusCode, Errors: env.Errors})
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fail(dnsprovider.KindTransient, fmt.Errorf("decode result: %w", err))
		}
	}
	return &env, nil
}

// unquote는 Cloudflare가 TXT content를 따옴표로 감싸 돌려줄 때 원래 값으로 되돌린다.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
