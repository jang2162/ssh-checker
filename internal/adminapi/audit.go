package adminapi

import (
	"net/http"
	"strconv"
	"time"

	"ssl-checker/internal/httpx"
)

const (
	defaultAuditLimit = 100
	maxAuditLimit     = 500
)

type auditView struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	Target   string    `json:"target"`
	SourceIP string    `json:"source_ip"`
	Detail   string    `json:"detail"`
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(r, defaultAuditLimit, maxAuditLimit)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit")
		return
	}
	var before int64
	if v := r.URL.Query().Get("before_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_before_id")
			return
		}
		before = n
	}
	recs, err := a.Store.ListAudit(r.Context(), before, limit)
	if err != nil {
		a.dbError(w, "list audit", err)
		return
	}
	out := make([]auditView, len(recs))
	for i, e := range recs {
		out[i] = auditView{ID: e.ID, At: e.At, Actor: e.Actor, Action: e.Action, Target: e.Target, SourceIP: e.SourceIP, Detail: e.Detail}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"entries": out})
}
