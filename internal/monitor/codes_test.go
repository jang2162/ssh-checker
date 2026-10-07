package monitor

import "testing"

func TestCodesCoverEveryCode(t *testing.T) {
	got := Codes()
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
	got := Codes()
	for i := 1; i < len(got); i++ {
		if got[i-1].Status.rank() < got[i].Status.rank() {
			t.Fatalf("%s (%s) listed before %s (%s)", got[i-1].Code, got[i-1].Status, got[i].Code, got[i].Status)
		}
	}
	if got[0].Status != StatusCritical || got[len(got)-1].Code != CodeNotChecked {
		t.Fatalf("first %s last %s", got[0].Code, got[len(got)-1].Code)
	}
}
