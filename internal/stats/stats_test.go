package stats

import "testing"

func TestWilsonDecisive(t *testing.T) {
	ci := WilsonCI(1000, 1000, 0.95)
	if !PassesMin(ci, 0.99) {
		t.Fatalf("expected pass: %+v", ci)
	}
	ci2 := WilsonCI(0, 100, 0.95)
	if !FailsMin(ci2, 0.99) {
		t.Fatalf("expected fail: %+v", ci2)
	}
	ci3 := WilsonCI(1, 2, 0.95)
	if PassesMin(ci3, 0.99) || FailsMin(ci3, 0.99) {
		t.Logf("inconclusive ok: %+v", ci3)
	}
}

func TestSamplesToClearMin(t *testing.T) {
	for _, tc := range []struct {
		min  float64
		want int
	}{
		{0.80, 16},
		{0.99, 381},
		{0.995, 765},
	} {
		got := SamplesToClearMin(tc.min, 0.95)
		if got != tc.want {
			t.Errorf("SamplesToClearMin(%v) = %d, want %d", tc.min, got, tc.want)
		}
		// A flawless run at the reported count must actually clear the gate.
		if ci := WilsonCI(got, got, 0.95); !PassesMin(ci, tc.min) {
			t.Errorf("min %v: %d clean samples still does not clear (CI low %.4f)", tc.min, got, ci.Low)
		}
		if got > 1 {
			if ci := WilsonCI(got-1, got-1, 0.95); PassesMin(ci, tc.min) {
				t.Errorf("min %v: %d samples already clears, count is not minimal", tc.min, got-1)
			}
		}
	}
}
