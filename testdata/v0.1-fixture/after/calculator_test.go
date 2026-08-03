package calculator

import "testing"

func TestMean(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	tests := []struct {
		name   string
		values []int
		want   float64
	}{
		{name: "empty", want: 0},
		{name: "positive", values: []int{2, 4, 6}, want: 4},
		{name: "negative", values: []int{-3, 3}, want: 0},
		{name: "large", values: []int{maxInt, maxInt}, want: float64(maxInt)},
		{name: "mixed extremes", values: []int{maxInt, minInt}, want: -0.5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Mean(test.values); got != test.want {
				t.Fatalf("Mean(%v) = %v, want %v", test.values, got, test.want)
			}
		})
	}
}

func TestMeanDoesNotMutateInput(t *testing.T) {
	values := []int{3, 1, 2}
	Mean(values)
	want := []int{3, 1, 2}
	for index := range values {
		if values[index] != want[index] {
			t.Fatalf("Mean mutated input: got %v, want %v", values, want)
		}
	}
}
