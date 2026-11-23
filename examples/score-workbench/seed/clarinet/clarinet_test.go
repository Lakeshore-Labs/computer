package clarinet

import "testing"

func TestWritten(t *testing.T) {
	cases := []struct{ concert, written int }{{60, 62}, {58, 60}, {70, 72}}
	for _, c := range cases {
		if got := Written(c.concert); got != c.written {
			t.Errorf("Written(%d) = %d, want %d", c.concert, got, c.written)
		}
	}
}

func TestName(t *testing.T) {
	if Name(60) != "C4" || Name(62) != "D4" {
		t.Errorf("Name wrong: %s %s", Name(60), Name(62))
	}
}
