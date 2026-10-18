package pdfscan

import "testing"

func TestRun(t *testing.T) {
	s := []byte("q 1 0 0 1 10 20 cm BT /F1 12 Tf 5 6 Td [<0041>] TJ ET Q BT /F2 9 Tf 1 0 0 1 30 40 Tm (x) Tj ET")
	got := Run(s)
	if len(got) != 2 {
		t.Fatalf("got %d ops", len(got))
	}
	if got[0].X != 15 || got[0].Y != 26 || got[0].Size != 12 {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].X != 30 || got[1].Y != 40 || got[1].Size != 9 {
		t.Errorf("second: %+v", got[1])
	}
}
