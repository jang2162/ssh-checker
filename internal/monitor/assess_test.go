package monitor

import (
	"slices"
	"testing"

	"ssl-checker/internal/cnamecheck"
)

func res(status Status, serial string, codes ...string) Result {
	return Result{Status: status, Serial: serial, Reasons: append([]string{}, codes...)}
}

func TestAssessTarget(t *testing.T) {
	cases := []struct {
		name   string
		latest []Result
		want   Assessment
	}{
		{"not checked", nil, Assessment{StatusUnknown, []string{CodeNotChecked}}},
		{"same serial", []Result{res(StatusOK, "01"), res(StatusOK, "01")}, Assessment{StatusOK, []string{}}},
		{"serial differs", []Result{res(StatusOK, "01"), res(StatusOK, "02")}, Assessment{StatusWarning, []string{CodeSerialMismatch}}},
		{"failed address has no serial", []Result{res(StatusCritical, "", CodeConnectFailed), res(StatusOK, "01")}, Assessment{StatusCritical, []string{CodeConnectFailed}}},
		{"worst reason first", []Result{res(StatusWarning, "01", CodeRenewalOverdue), res(StatusCritical, "02", CodeChainInvalid)},
			Assessment{StatusCritical, []string{CodeChainInvalid, CodeRenewalOverdue, CodeSerialMismatch}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AssessTarget(tc.latest)
			if got.Status != tc.want.Status || !slices.Equal(got.Reasons, tc.want.Reasons) {
				t.Fatalf("AssessTarget = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAssessDomain(t *testing.T) {
	ok := Assessment{StatusOK, []string{}}
	crit := Assessment{StatusCritical, []string{CodeCertExpiresSoon}}
	cases := []struct {
		name    string
		targets []Assessment
		acme    *ACMEHealth
		want    Assessment
	}{
		{"nothing", nil, nil, Assessment{StatusUnknown, []string{}}},
		{"acme ok without targets stays unknown", nil, &ACMEHealth{CNAMEStatus: cnamecheck.StatusOK}, Assessment{StatusUnknown, []string{}}},
		{"cname missing without targets", nil, &ACMEHealth{CNAMEStatus: cnamecheck.StatusMissing}, Assessment{StatusWarning, []string{CodeCNAMEMissing}}},
		{"cname error is not a warning", []Assessment{ok}, &ACMEHealth{CNAMEStatus: cnamecheck.StatusError}, Assessment{StatusOK, []string{}}},
		{"two failures", []Assessment{ok}, &ACMEHealth{CNAMEStatus: cnamecheck.StatusOK, ConsecutiveFailures: 2}, Assessment{StatusOK, []string{}}},
		{"three failures", []Assessment{ok}, &ACMEHealth{CNAMEStatus: cnamecheck.StatusOK, ConsecutiveFailures: 3}, Assessment{StatusWarning, []string{CodeTXTUpdateFailing}}},
		{"critical target and mismatch", []Assessment{ok, crit}, &ACMEHealth{CNAMEStatus: cnamecheck.StatusMismatch},
			Assessment{StatusCritical, []string{CodeCertExpiresSoon, CodeCNAMEMismatch}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AssessDomain(tc.targets, tc.acme)
			if got.Status != tc.want.Status || !slices.Equal(got.Reasons, tc.want.Reasons) {
				t.Fatalf("AssessDomain = %+v, want %+v", got, tc.want)
			}
		})
	}
}
