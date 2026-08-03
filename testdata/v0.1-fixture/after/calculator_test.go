package calculator

import "testing"

func TestMean(t *testing.T) {
	tests := []struct {
		name   string
		values []int
		want   float64
	}{
		{name: "empty", want: 0},
		{name: "positive", values: []int{2, 4, 6}, want: 4},
		{name: "negative", values: []int{-3, 3}, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Mean(test.values); got != test.want {
				t.Fatalf("Mean(%v) = %v, want %v", test.values, got, test.want)
			}
		})
	}
}
