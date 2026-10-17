package metrics

import (
	"math"
	"testing"
)

func TestWilcoxon(t *testing.T) {
	tests := []struct {
		name      string
		a, b      []float64
		wantNil   bool
		wantStars string
		wantP     float64 // approx upper bound
	}{
		{
			name:    "too few pairs returns nil",
			a:       []float64{0.5, 0.6, 0.7},
			b:       []float64{0.5, 0.6, 0.7},
			wantNil: true,
		},
		{
			name:    "all tied returns nil",
			a:       []float64{0.5, 0.5, 0.5, 0.5, 0.5},
			b:       []float64{0.5, 0.5, 0.5, 0.5, 0.5},
			wantNil: true,
		},
		{
			name:      "no significant difference",
			a:         []float64{0.50, 0.55, 0.48, 0.52, 0.53, 0.49, 0.51, 0.54, 0.47, 0.56},
			b:         []float64{0.51, 0.54, 0.50, 0.51, 0.52, 0.51, 0.50, 0.53, 0.49, 0.55},
			wantStars: "",
		},
		{
			name:      "engine A clearly dominates — significant",
			a:         []float64{0.90, 0.85, 0.88, 0.92, 0.87, 0.91, 0.86, 0.89, 0.93, 0.84},
			b:         []float64{0.30, 0.25, 0.28, 0.32, 0.27, 0.31, 0.26, 0.29, 0.33, 0.24},
			wantStars: "**",
			wantP:     0.01,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Wilcoxon(tc.a, tc.b)
			if tc.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if stars := SignificanceStars(got.P); stars != tc.wantStars {
				t.Errorf("stars: got %q, want %q (p=%.4f)", stars, tc.wantStars, got.P)
			}
			if tc.wantP > 0 && got.P > tc.wantP {
				t.Errorf("p-value %.4f exceeds expected upper bound %.4f", got.P, tc.wantP)
			}
			if math.IsNaN(got.P) || math.IsInf(got.P, 0) {
				t.Errorf("p-value is not finite: %v", got.P)
			}
		})
	}
}

// Expected W and p come from scipy 1.18 scipy.stats.wilcoxon on the same
// differences rounded to 1e-12: method="exact" for the untied cases up to
// n=50, method="approx" with correction=True otherwise.
func TestWilcoxon_MatchesScipy(t *testing.T) {
	untiedDifferences := func(n int) []float64 {
		d := make([]float64, n)
		for i := range d {
			d[i] = float64(i+1) / 100
			if i%3 == 0 {
				d[i] = -d[i]
			}
		}
		return d
	}

	tests := []struct {
		name  string
		a, b  []float64
		wantW float64
		wantP float64
	}{
		{
			name: "reciprocal ranks whose equal differences round apart",
			a:    []float64{1.0 / 6, 0, 1.0 / 6, 0, 1.0 / 6, 0.5, 0.5, 0.5, 0, 0},
			b:    []float64{0.5, 1.0 / 3, 1.0 / 3, 1.0 / 6, 0.5, 1.0 / 3, 1.0 / 3, 1.0 / 3, 1.0 / 6, 1.0 / 3},
			// scipy approx, correction=True; the unrounded differences give W=15.
			wantW: 10.5,
			wantP: 0.08305214716413864,
		},
		{
			name:  "seven untied pairs use the exact distribution",
			a:     []float64{0.9, 0.8, 0.75, 0.6, 0.55, 0.4, 0.35},
			b:     []float64{0.1, 0.85, 0.5, 0.2, 0.4, 0.37, 0.0},
			wantW: 2,
			wantP: 0.046875,
		},
		{
			name:  "twelve untied pairs use the exact distribution",
			a:     []float64{0.11, -0.02, 0.33, 0.44, -0.05, 0.61, 0.72, 0.83, 0.94, 1.05, -0.16, 1.27},
			b:     make([]float64, 12),
			wantW: 7,
			wantP: 0.00927734375,
		},
		{
			name:  "fifty untied pairs still use the exact distribution",
			a:     untiedDifferences(50),
			b:     make([]float64, 50),
			wantW: 425,
			wantP: 0.03996834652842374,
		},
		{
			name:  "fifty-one untied pairs use the normal approximation",
			a:     untiedDifferences(51),
			b:     make([]float64, 51),
			wantW: 425,
			wantP: 0.026000896360930642,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Wilcoxon(tc.a, tc.b)
			if got == nil {
				t.Fatal("expected a result")
			}
			if math.Abs(got.W-tc.wantW) > 1e-9 {
				t.Errorf("W = %v, want %v", got.W, tc.wantW)
			}
			if math.Abs(got.P-tc.wantP) > 1e-12 {
				t.Errorf("p = %v, want %v", got.P, tc.wantP)
			}
		})
	}
}

// Expected values from statsmodels multipletests(method="holm").
func TestHolm(t *testing.T) {
	tests := []struct {
		raw, want []float64
	}{
		{raw: []float64{0.01, 0.04, 0.03}, want: []float64{0.03, 0.06, 0.06}},
		{raw: []float64{0.004, 0.02, 0.5, 0.012}, want: []float64{0.016, 0.04, 0.5, 0.036}},
		{raw: []float64{0.3, 0.6}, want: []float64{0.6, 0.6}},
	}
	for _, tc := range tests {
		got := Holm(tc.raw)
		if len(got) != len(tc.want) {
			t.Fatalf("Holm(%v) = %v, want %v", tc.raw, got, tc.want)
		}
		for i := range got {
			if math.Abs(got[i]-tc.want[i]) > 1e-12 {
				t.Errorf("Holm(%v) = %v, want %v", tc.raw, got, tc.want)
				break
			}
		}
	}
}
