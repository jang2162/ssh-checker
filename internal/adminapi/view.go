package adminapi

import (
	"math"
	"time"

	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

type acmeSummary struct {
	Enabled     bool              `json:"enabled"`
	CNAMEStatus cnamecheck.Status `json:"cname_status,omitempty"`
}

type domainSummary struct {
	ID   int64  `json:"id"`
	FQDN string `json:"fqdn"`
	Memo string `json:"memo"`
	monitor.Assessment
	EarliestNotAfter *time.Time  `json:"earliest_not_after"`
	DaysLeft         *int        `json:"days_left"`
	Targets          int         `json:"targets"`
	ACME             acmeSummary `json:"acme"`
	LastCheckedAt    *time.Time  `json:"last_checked_at"`
}

type targetView struct {
	ID       int64    `json:"id"`
	Hostname string   `json:"hostname"`
	Port     int      `json:"port"`
	SNI      string   `json:"sni"`
	IPs      []string `json:"ips"`
	Memo     string   `json:"memo"`
	monitor.Assessment
	Results []monitor.Result `json:"results"`
}

type acmeDetail struct {
	Enabled             bool              `json:"enabled"`
	Username            string            `json:"username"`
	Subdomain           string            `json:"subdomain"`
	FullDomain          string            `json:"fulldomain"`
	CNAMERecord         string            `json:"cname_record"`
	AllowFrom           []string          `json:"allowfrom"`
	CNAMEStatus         cnamecheck.Status `json:"cname_status"`
	CNAMEValue          string            `json:"cname_value"`
	CNAMECheckedAt      *time.Time        `json:"cname_checked_at"`
	LastUpdateAt        *time.Time        `json:"last_update_at"`
	LastFailureAt       *time.Time        `json:"last_failure_at"`
	ConsecutiveFailures int               `json:"consecutive_failures"`
}

type domainDetail struct {
	ID        int64     `json:"id"`
	FQDN      string    `json:"fqdn"`
	Memo      string    `json:"memo"`
	CreatedAt time.Time `json:"created_at"`
	monitor.Assessment
	Targets []targetView `json:"targets"`
	ACME    any          `json:"acme"` // 꺼져 있으면 {"enabled":false}만
}

// assess는 도메인의 대상별 판정과 도메인 판정을 함께 계산한다. acme가 nil이면 ACME가 꺼져 있다.
func assess(targets []store.Target, latest map[int64][]monitor.Result, acme *store.ACMEState) (monitor.Assessment, []monitor.Assessment) {
	per := make([]monitor.Assessment, len(targets))
	for i, t := range targets {
		per[i] = monitor.AssessTarget(latest[t.ID])
	}
	var h *monitor.ACMEHealth
	if acme != nil {
		h = &monitor.ACMEHealth{CNAMEStatus: acme.CNAMEStatus, ConsecutiveFailures: acme.ConsecutiveFailures}
	}
	return monitor.AssessDomain(per, h), per
}

func summarize(d store.Domain, targets []store.Target, latest map[int64][]monitor.Result, acme *store.ACMEState, now time.Time) domainSummary {
	overall, _ := assess(targets, latest, acme)
	out := domainSummary{ID: d.ID, FQDN: d.FQDN, Memo: d.Memo, Assessment: overall, Targets: len(targets), ACME: acmeSummary{Enabled: acme != nil}}
	if acme != nil {
		out.ACME.CNAMEStatus = acme.CNAMEStatus
	}
	for _, t := range targets {
		for _, r := range latest[t.ID] {
			if r.HasCert() && (out.EarliestNotAfter == nil || r.NotAfter.Before(*out.EarliestNotAfter)) {
				na := r.NotAfter
				out.EarliestNotAfter = &na
			}
			if out.LastCheckedAt == nil || r.CheckedAt.After(*out.LastCheckedAt) {
				c := r.CheckedAt
				out.LastCheckedAt = &c
			}
		}
	}
	if out.EarliestNotAfter != nil {
		days := int(math.Floor(out.EarliestNotAfter.Sub(now).Hours() / 24))
		out.DaysLeft = &days
	}
	return out
}

func (a *API) acmeView(fqdn string, st *store.ACMEState) any {
	if st == nil {
		return map[string]bool{"enabled": false}
	}
	full := st.Subdomain + "." + a.Zone
	allow := make([]string, 0, len(st.AllowFrom))
	for _, p := range st.AllowFrom {
		allow = append(allow, p.String())
	}
	return acmeDetail{
		Enabled: true, Username: st.Username, Subdomain: st.Subdomain, FullDomain: full,
		CNAMERecord: cnamecheck.ChallengeName(fqdn) + ". CNAME " + full + ".",
		AllowFrom:   allow, CNAMEStatus: st.CNAMEStatus, CNAMEValue: st.CNAMEValue, CNAMECheckedAt: st.CNAMECheckedAt,
		LastUpdateAt: st.LastUpdateAt, LastFailureAt: st.LastFailureAt, ConsecutiveFailures: st.ConsecutiveFailures,
	}
}

func targetViews(targets []store.Target, latest map[int64][]monitor.Result, per []monitor.Assessment) []targetView {
	out := make([]targetView, len(targets))
	for i, t := range targets {
		ips := make([]string, 0, len(t.IPs))
		for _, ip := range t.IPs {
			ips = append(ips, ip.String())
		}
		rs := latest[t.ID]
		if rs == nil {
			rs = []monitor.Result{}
		}
		out[i] = targetView{ID: t.ID, Hostname: t.Hostname, Port: t.Port, SNI: t.SNI, IPs: ips, Memo: t.Memo, Assessment: per[i], Results: rs}
	}
	return out
}
