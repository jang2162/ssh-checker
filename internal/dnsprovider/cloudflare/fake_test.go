package cloudflare

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strconv"
	"sync"
	"testing"
)

// fakeCF는 테스트에 필요한 Cloudflare DNS API 일부를 흉내 낸다.
type fakeCF struct {
	token      string
	zoneID     string
	zoneName   string
	quote      bool // content를 따옴표로 감싸 저장·반환한다
	maxPerPage int

	ignoreNameFilter bool // name 쿼리를 무시하고 모든 TXT를 돌려준다

	mu         sync.Mutex
	nextID     int
	records    map[string]record
	failStatus int // 0이 아니면 모든 요청에 이 상태 코드로 실패한다
	mutations  int
	srv        *httptest.Server
}

func newFakeCF(t *testing.T) *fakeCF {
	f := &fakeCF{token: "tok", zoneID: "zone123", zoneName: "acme-chk.test", maxPerPage: 100, records: map[string]record{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /zones", f.listZones)
	mux.HandleFunc("GET /zones/{zid}/dns_records", f.listRecords)
	mux.HandleFunc("POST /zones/{zid}/dns_records", f.createRecord)
	mux.HandleFunc("PUT /zones/{zid}/dns_records/{rid}", f.updateRecord)
	mux.HandleFunc("DELETE /zones/{zid}/dns_records/{rid}", f.deleteRecord)
	f.srv = httptest.NewServer(f.guard(mux))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCF) provider() *Provider {
	return New(Config{Token: f.token, Zone: f.zoneName, BaseURL: f.srv.URL})
}

func (f *fakeCF) values(fqdn string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, r := range f.records {
		if r.Name == fqdn {
			out = append(out, unquote(r.Content))
		}
	}
	slices.Sort(out)
	return out
}

func writeCF(w http.ResponseWriter, status int, success bool, result any, info any, errs ...apiError) {
	if errs == nil {
		errs = []apiError{}
	}
	body := map[string]any{"success": success, "errors": errs, "messages": []any{}, "result": result}
	if info != nil {
		body["result_info"] = info
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeCF) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		status := f.failStatus
		f.mu.Unlock()
		if status != 0 {
			writeCF(w, status, false, nil, nil, apiError{Code: 9999, Message: "injected failure"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			writeCF(w, http.StatusForbidden, false, nil, nil, apiError{Code: 10000, Message: "Authentication error"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (f *fakeCF) zoneOK(w http.ResponseWriter, r *http.Request) bool {
	if r.PathValue("zid") != f.zoneID {
		writeCF(w, http.StatusNotFound, false, nil, nil, apiError{Code: 7003, Message: "Could not route to zone"})
		return false
	}
	return true
}

func (f *fakeCF) listZones(w http.ResponseWriter, r *http.Request) {
	res := []map[string]string{}
	if r.URL.Query().Get("name") == f.zoneName {
		res = append(res, map[string]string{"id": f.zoneID, "name": f.zoneName})
	}
	writeCF(w, http.StatusOK, true, res, nil)
}

func (f *fakeCF) listRecords(w http.ResponseWriter, r *http.Request) {
	if !f.zoneOK(w, r) {
		return
	}
	q := r.URL.Query()
	f.mu.Lock()
	defer f.mu.Unlock()
	matched := []record{}
	for _, rec := range f.records {
		if t := q.Get("type"); t != "" && rec.Type != t {
			continue
		}
		if n := q.Get("name"); n != "" && rec.Name != n && !f.ignoreNameFilter {
			continue
		}
		matched = append(matched, rec)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
	per, _ := strconv.Atoi(q.Get("per_page"))
	if per <= 0 || per > f.maxPerPage {
		per = f.maxPerPage
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page <= 0 {
		page = 1
	}
	total := (len(matched) + per - 1) / per
	start := min((page-1)*per, len(matched))
	end := min(start+per, len(matched))
	writeCF(w, http.StatusOK, true, matched[start:end], map[string]int{"page": page, "per_page": per, "total_pages": total})
}

func (f *fakeCF) decode(w http.ResponseWriter, r *http.Request) (record, bool) {
	var in record
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Type != "TXT" || in.Name == "" {
		writeCF(w, http.StatusBadRequest, false, nil, nil, apiError{Code: 1004, Message: "DNS Validation Error"})
		return record{}, false
	}
	if in.TTL != 1 && in.TTL < 60 {
		writeCF(w, http.StatusBadRequest, false, nil, nil, apiError{Code: 1004, Message: "TTL must be at least 60"})
		return record{}, false
	}
	if f.quote {
		in.Content = `"` + in.Content + `"`
	}
	return in, true
}

// duplicate는 같은 이름·내용의 레코드가 이미 있는지 본다. Cloudflare는 이를 81058로 거부한다.
func (f *fakeCF) duplicate(in record, exceptID string) bool {
	for id, rec := range f.records {
		if id != exceptID && rec.Name == in.Name && rec.Content == in.Content {
			return true
		}
	}
	return false
}

func (f *fakeCF) createRecord(w http.ResponseWriter, r *http.Request) {
	if !f.zoneOK(w, r) {
		return
	}
	in, ok := f.decode(w, r)
	if !ok {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.duplicate(in, "") {
		writeCF(w, http.StatusBadRequest, false, nil, nil, apiError{Code: 81058, Message: "An identical record already exists."})
		return
	}
	f.nextID++
	in.ID = fmt.Sprintf("rec%04d", f.nextID)
	f.records[in.ID] = in
	f.mutations++
	writeCF(w, http.StatusOK, true, in, nil)
}

func (f *fakeCF) updateRecord(w http.ResponseWriter, r *http.Request) {
	if !f.zoneOK(w, r) {
		return
	}
	in, ok := f.decode(w, r)
	if !ok {
		return
	}
	rid := r.PathValue("rid")
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.records[rid]; !exists {
		writeCF(w, http.StatusNotFound, false, nil, nil, apiError{Code: 81044, Message: "Record does not exist."})
		return
	}
	if f.duplicate(in, rid) {
		writeCF(w, http.StatusBadRequest, false, nil, nil, apiError{Code: 81058, Message: "An identical record already exists."})
		return
	}
	in.ID = rid
	f.records[rid] = in
	f.mutations++
	writeCF(w, http.StatusOK, true, in, nil)
}

func (f *fakeCF) deleteRecord(w http.ResponseWriter, r *http.Request) {
	if !f.zoneOK(w, r) {
		return
	}
	rid := r.PathValue("rid")
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.records[rid]; !exists {
		writeCF(w, http.StatusNotFound, false, nil, nil, apiError{Code: 81044, Message: "Record does not exist."})
		return
	}
	delete(f.records, rid)
	f.mutations++
	writeCF(w, http.StatusOK, true, map[string]string{"id": rid}, nil)
}
