package calculator

import "math/big"

func Sum(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func Mean(values []int) float64 {
	if len(values) == 0 {
		return 0
	}
	total := new(big.Int)
	for _, value := range values {
		total.Add(total, big.NewInt(int64(value)))
	}
	mean := new(big.Rat).SetFrac(total, big.NewInt(int64(len(values))))
	result, _ := mean.Float64()
	return result
}
