// Package monitor는 TLS 인증서를 점검하고 상태를 판정한다.
package monitor

import "slices"

// Status는 판정 결과다. 나쁜 순서: critical > warning > info > ok > unknown.
type Status string

const (
	StatusUnknown  Status = "unknown"
	StatusOK       Status = "ok"
	StatusInfo     Status = "info"
	StatusWarning  Status = "warning"
	StatusCritical Status = "critical"
)

func (s Status) rank() int {
	switch s {
	case StatusOK:
		return 1
	case StatusInfo:
		return 2
	case StatusWarning:
		return 3
	case StatusCritical:
		return 4
	}
	return 0
}

// Valid는 s가 정의된 상태 값인지 알려 준다. 조회 API의 status 필터 검사에 쓴다.
func (s Status) Valid() bool { return s == StatusUnknown || s.rank() > 0 }

// Worst는 가장 나쁜 상태다. 인자가 없으면 unknown이다.
func Worst(ss ...Status) Status {
	w := StatusUnknown
	for _, s := range ss {
		if s.rank() > w.rank() {
			w = s
		}
	}
	return w
}

// 판정 코드. 화면과 llms.txt가 이 문자열을 그대로 보여 준다.
const (
	CodeConnectFailed    = "CONNECT_FAILED"
	CodeCertExpired      = "CERT_EXPIRED"
	CodeCertExpiresSoon  = "CERT_EXPIRES_SOON"
	CodeChainInvalid     = "CHAIN_INVALID"
	CodeHostnameMismatch = "HOSTNAME_MISMATCH"
	CodeRenewalOverdue   = "RENEWAL_OVERDUE"
	CodeTLSOutdated      = "TLS_OUTDATED"
	CodeWeakKey          = "WEAK_KEY"
	CodeSerialMismatch   = "SERIAL_MISMATCH"
	CodeNotChecked       = "NOT_CHECKED"
	CodeCNAMEMissing     = "CNAME_MISSING"
	CodeCNAMEMismatch    = "CNAME_MISMATCH"
	CodeTXTUpdateFailing = "TXT_UPDATE_FAILING"
)

var codeStatus = map[string]Status{
	CodeConnectFailed:    StatusCritical,
	CodeCertExpired:      StatusCritical,
	CodeCertExpiresSoon:  StatusCritical,
	CodeChainInvalid:     StatusCritical,
	CodeHostnameMismatch: StatusCritical,
	CodeRenewalOverdue:   StatusWarning,
	CodeTLSOutdated:      StatusWarning,
	CodeSerialMismatch:   StatusWarning,
	CodeCNAMEMissing:     StatusWarning,
	CodeCNAMEMismatch:    StatusWarning,
	CodeTXTUpdateFailing: StatusWarning,
	CodeWeakKey:          StatusInfo,
	CodeNotChecked:       StatusUnknown,
}

// CodeStatus는 코드 하나가 뜻하는 상태다. 모르는 코드는 unknown이다.
func CodeStatus(code string) Status {
	if s, ok := codeStatus[code]; ok {
		return s
	}
	return StatusUnknown
}

// sortCodes는 나쁜 코드가 앞에 오도록 안정 정렬한다.
func sortCodes(codes []string) {
	slices.SortStableFunc(codes, func(a, b string) int { return CodeStatus(b).rank() - CodeStatus(a).rank() })
}

func appendUnique(dst []string, codes ...string) []string {
	for _, c := range codes {
		if !slices.Contains(dst, c) {
			dst = append(dst, c)
		}
	}
	return dst
}
