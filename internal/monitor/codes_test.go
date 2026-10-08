package monitor

import (
	"regexp"
	"testing"

	"ssl-checker/internal/lang"
)

func TestCodesCoverEveryCode(t *testing.T) {
	for _, l := range []lang.Lang{lang.EN, lang.KO} {
		checkCodesCoverEveryCode(t, l)
	}
}

func checkCodesCoverEveryCode(t *testing.T, l lang.Lang) {
	t.Helper()
	got := Codes(l)
	if len(got) != len(codeStatus) {
		t.Fatalf("len(Codes()) = %d, want %d", len(got), len(codeStatus))
	}
	seen := map[string]bool{}
	for _, c := range got {
		if seen[c.Code] {
			t.Errorf("%s listed twice", c.Code)
		}
		seen[c.Code] = true
		if c.Meaning == "" {
			t.Errorf("%s has no meaning", c.Code)
		}
		if c.Status != CodeStatus(c.Code) {
			t.Errorf("%s status = %s, want %s", c.Code, c.Status, CodeStatus(c.Code))
		}
	}
	for code := range codeStatus {
		if !seen[code] {
			t.Errorf("%s missing from Codes()", code)
		}
	}
}

func TestCodesWorstFirst(t *testing.T) {
	got := Codes(lang.EN)
	for i := 1; i < len(got); i++ {
		if got[i-1].Status.rank() < got[i].Status.rank() {
			t.Fatalf("%s (%s) listed before %s (%s)", got[i-1].Code, got[i-1].Status, got[i].Code, got[i].Status)
		}
	}
	if got[0].Status != StatusCritical || got[len(got)-1].Code != CodeNotChecked {
		t.Fatalf("first %s last %s", got[0].Code, got[len(got)-1].Code)
	}
}

// 영어 설명에는 한글이 없고, 한국어 설명에는 한글이 있다. 언어마다 같은 코드를 담는다.
func TestCodeMeaningsFollowLanguage(t *testing.T) {
	hangul := regexp.MustCompile("[가-힣]")
	for code := range codeStatus {
		if hangul.MatchString(codeMeaningsEN[code]) {
			t.Errorf("%s: English meaning contains Hangul: %q", code, codeMeaningsEN[code])
		}
		if !hangul.MatchString(codeMeaningsKO[code]) {
			t.Errorf("%s: Korean meaning has no Hangul: %q", code, codeMeaningsKO[code])
		}
	}
	if len(codeMeaningsEN) != len(codeMeaningsKO) {
		t.Errorf("tables differ in size: en %d, ko %d", len(codeMeaningsEN), len(codeMeaningsKO))
	}
	if Codes(lang.EN)[0].Meaning == Codes(lang.KO)[0].Meaning {
		t.Error("Codes should return different text per language")
	}
}
