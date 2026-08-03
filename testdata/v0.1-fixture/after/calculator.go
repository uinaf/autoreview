package calculator

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
	total := 0.0
	for _, value := range values {
		total += float64(value)
	}
	return total / float64(len(values))
}
