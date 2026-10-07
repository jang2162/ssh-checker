package monitor

import "time"

const (
	expiresSoonWithin = 7 * 24 * time.Hour
	minRSABits        = 2048
	minECDSABits      = 256
)

// Evaluate는 접속 주소 하나의 점검 결과를 판정한다. 코드는 나쁜 것부터 돌려준다.
func Evaluate(r Result, now time.Time) (Status, []string) {
	codes := []string{}
	if r.Error != "" || !r.HasCert() {
		codes = append(codes, CodeConnectFailed)
	} else {
		left := r.NotAfter.Sub(now)
		third := r.NotAfter.Sub(r.NotBefore) / 3
		switch {
		case left <= 0:
			codes = append(codes, CodeCertExpired)
		case left < min(expiresSoonWithin, third):
			// 수명이 짧은 인증서(예: 6일짜리)는 7일 고정 기준이면 발급 직후부터 긴급이 되므로 수명의 1/3을 넘지 않게 한다.
			codes = append(codes, CodeCertExpiresSoon)
		case left < third:
			// ACME 클라이언트는 보통 수명이 1/3 남았을 때 갱신하므로, 정상이면 이미 바뀌었어야 한다.
			codes = append(codes, CodeRenewalOverdue)
		}
		if !r.ChainOK {
			codes = append(codes, CodeChainInvalid)
		}
		if !r.HostnameOK {
			codes = append(codes, CodeHostnameMismatch)
		}
		switch r.TLSVersion {
		case "SSL 3.0", "TLS 1.0", "TLS 1.1":
			codes = append(codes, CodeTLSOutdated)
		}
		if (r.KeyAlg == "RSA" && r.KeyBits < minRSABits) || (r.KeyAlg == "ECDSA" && r.KeyBits < minECDSABits) {
			codes = append(codes, CodeWeakKey)
		}
	}
	sortCodes(codes)
	st := StatusOK
	for _, c := range codes {
		st = Worst(st, CodeStatus(c))
	}
	return st, codes
}
