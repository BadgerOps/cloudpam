package discovery

import (
	"context"
	"testing"
	"time"
)

func TestNilLimiterNeverWaits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var l *Limiter
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("nil limiter Wait() = %v, want nil", err)
	}
	if NewLimiter(0) != nil {
		t.Fatal("NewLimiter(0) should be nil (unlimited)")
	}
}

func TestLimiterHoldsTheSecondCallWithinASecond(t *testing.T) {
	l := NewLimiter(1)
	start := time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait() = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("first call waited %s, want immediate", elapsed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx); err == nil {
		t.Fatal("second Wait() within one second = nil, want an error before the deadline")
	}
}

func TestLimiterCapsTheBurstAtMaxRPS(t *testing.T) {
	l := NewLimiter(1e300)
	for i := 0; i < 3; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("Wait() %d = %v, want nil", i, err)
		}
	}
}

func TestParseRPS(t *testing.T) {
	cases := []struct {
		in      string
		want    float64
		wantErr bool
	}{
		{in: "", want: 0},
		{in: "  ", want: 0},
		{in: "0", want: 0},
		{in: "2.5", want: 2.5},
		{in: " 10 ", want: 10},
		{in: "1000000", want: 1000000},
		{in: "-1", wantErr: true},
		{in: "NaN", wantErr: true},
		{in: "Inf", wantErr: true},
		{in: "1000001", wantErr: true},
		{in: "ten", wantErr: true},
	}
	for _, tc := range cases {
		got, err := ParseRPS(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseRPS(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseRPS(%q) = %v, %v, want %v", tc.in, got, err, tc.want)
		}
	}
}
