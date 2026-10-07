package monitor

import "ssl-checker/internal/cnamecheck"

// Assessment는 대상이나 도메인의 종합 판정이다.
type Assessment struct {
	Status  Status   `json:"status"`
	Reasons []string `json:"reasons"`
}

func (a *Assessment) add(code string) {
	a.Status = Worst(a.Status, CodeStatus(code))
	a.Reasons = appendUnique(a.Reasons, code)
}

// TXTFailureThreshold번 이상 /update가 연달아 실패하면 TXT_UPDATE_FAILING을 붙인다.
const TXTFailureThreshold = 3

// AssessTarget은 대상의 접속 주소별 최신 결과를 합친다. 인증서를 받은 주소끼리 serial이 다르면
// 일부 서버에 새 인증서가 배포되지 않은 것이다.
func AssessTarget(latest []Result) Assessment {
	if len(latest) == 0 {
		return Assessment{Status: StatusUnknown, Reasons: []string{CodeNotChecked}}
	}
	a := Assessment{Status: StatusOK, Reasons: []string{}}
	serials := map[string]bool{}
	for _, r := range latest {
		a.Status = Worst(a.Status, r.Status)
		a.Reasons = appendUnique(a.Reasons, r.Reasons...)
		if r.Serial != "" {
			serials[r.Serial] = true
		}
	}
	if len(serials) > 1 {
		a.add(CodeSerialMismatch)
	}
	sortCodes(a.Reasons)
	return a
}

// ACMEHealth는 ACME가 켜진 도메인의 위임 상태다.
type ACMEHealth struct {
	CNAMEStatus         cnamecheck.Status
	ConsecutiveFailures int
}

// AssessDomain은 대상 판정과 ACME 상태를 합친다. acme가 nil이면 ACME가 꺼진 도메인이다.
// CNAME 조회 실패(error)는 일시적일 수 있어 경고로 보지 않는다.
func AssessDomain(targets []Assessment, acme *ACMEHealth) Assessment {
	a := Assessment{Status: StatusUnknown, Reasons: []string{}}
	for _, t := range targets {
		a.Status = Worst(a.Status, t.Status)
		a.Reasons = appendUnique(a.Reasons, t.Reasons...)
	}
	if acme != nil {
		switch acme.CNAMEStatus {
		case cnamecheck.StatusMissing:
			a.add(CodeCNAMEMissing)
		case cnamecheck.StatusMismatch:
			a.add(CodeCNAMEMismatch)
		}
		if acme.ConsecutiveFailures >= TXTFailureThreshold {
			a.add(CodeTXTUpdateFailing)
		}
	}
	sortCodes(a.Reasons)
	return a
}
