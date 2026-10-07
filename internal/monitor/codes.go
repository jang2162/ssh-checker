package monitor

import (
	"slices"
	"strings"
)

// CodeInfo는 판정 코드 하나의 설명이다. 화면(/api/codes)과 /llms.txt가 같은 표를 쓴다.
type CodeInfo struct {
	Code    string `json:"code"`
	Status  Status `json:"status"`
	Meaning string `json:"meaning"`
}

// codeMeanings는 코드별 한국어 설명이다. 새 코드를 만들면 여기에도 적는다(테스트가 빠진 코드를 잡는다).
var codeMeanings = map[string]string{
	CodeConnectFailed:    "접속이나 TLS 핸드셰이크에 실패했습니다(10초 제한). 아주 오래된 서버도 여기로 나오니 error 필드를 확인합니다.",
	CodeCertExpired:      "인증서가 만료됐습니다.",
	CodeCertExpiresSoon:  "만료까지 min(7일, 수명의 1/3) 미만 남았습니다.",
	CodeChainInvalid:     "인증서 체인 검증에 실패했습니다(자체 서명 포함).",
	CodeHostnameMismatch: "인증서가 접속한 이름을 덮지 않습니다.",
	CodeRenewalOverdue:   "남은 기간이 수명의 1/3 미만입니다. 정상이면 이미 갱신됐을 시점입니다.",
	CodeTLSOutdated:      "TLS 1.2 미만으로 협상됐습니다.",
	CodeSerialMismatch:   "같은 대상의 서버들이 서로 다른 인증서를 냅니다(일부 서버에 배포가 빠짐).",
	CodeCNAMEMissing:     "ACME를 켰는데 _acme-challenge CNAME이 없습니다.",
	CodeCNAMEMismatch:    "_acme-challenge CNAME이 다른 곳을 가리킵니다.",
	CodeTXTUpdateFailing: "/update가 3번 이상 연달아 실패했습니다(DNS 공급자나 DB 오류).",
	CodeWeakKey:          "RSA 2048비트 미만 또는 ECDSA 256비트 미만 키입니다.",
	CodeNotChecked:       "아직 점검 결과가 없습니다.",
}

// Codes는 모든 판정 코드를 나쁜 상태부터 돌려준다. 같은 상태 안에서는 코드 이름순이다.
func Codes() []CodeInfo {
	out := make([]CodeInfo, 0, len(codeStatus))
	for code, st := range codeStatus {
		out = append(out, CodeInfo{Code: code, Status: st, Meaning: codeMeanings[code]})
	}
	slices.SortFunc(out, func(a, b CodeInfo) int {
		if d := b.Status.rank() - a.Status.rank(); d != 0 {
			return d
		}
		return strings.Compare(a.Code, b.Code)
	})
	return out
}
