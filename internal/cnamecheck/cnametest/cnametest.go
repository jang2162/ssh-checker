// Package cnametest는 테스트에서 쓰는 최소 UDP DNS 서버를 띄운다.
package cnametest

import (
	"net"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// Answer는 이름 하나에 대한 응답이다. CNAME이 있으면 CNAME 레코드 하나를 돌려주고,
// RCode로 NXDOMAIN 등을 흉내 낸다. Drop이면 응답하지 않는다(타임아웃 시험용).
type Answer struct {
	CNAME string
	RCode dnsmessage.RCode
	Drop  bool
}

// Serve는 answers로 응답하는 서버를 띄우고 주소를 돌려준다. 키는 소문자 FQDN에 끝 점을 붙인다.
func Serve(t testing.TB, answers map[string]Answer) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var q dnsmessage.Message
			if q.Unpack(buf[:n]) != nil || len(q.Questions) != 1 {
				continue
			}
			a := answers[strings.ToLower(q.Questions[0].Name.String())]
			if a.Drop {
				continue
			}
			resp := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: q.ID, Response: true, RCode: a.RCode},
				Questions: q.Questions,
			}
			if a.CNAME != "" {
				resp.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{Name: q.Questions[0].Name, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   &dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName(a.CNAME)},
				}}
			}
			out, err := resp.Pack()
			if err != nil {
				continue
			}
			_, _ = pc.WriteTo(out, addr)
		}
	}()
	return pc.LocalAddr().String()
}
