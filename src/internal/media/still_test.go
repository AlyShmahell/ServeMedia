package media

import "testing"

func TestStillSeekTimes(t *testing.T) {
	got := stillSeekTimes(0)
	if len(got) == 0 || got[len(got)-1] != 0 {
		t.Fatalf("zero duration %#v", got)
	}
	got = stillSeekTimes(120)
	if len(got) < 2 || got[0] != 12 {
		t.Fatalf("10 percent %#v", got)
	}
	got = stillSeekTimes(400)
	if got[0] != 30 {
		t.Fatalf("clamped %#v", got)
	}
}
