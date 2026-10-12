package shop

// LineTotal returns the price of one order line in cents, after the quantity discount.
func LineTotal(unitCents, qty int) int {
	total := unitCents * qty
	switch {
	case qty >= 100:
		total = total * 85 / 100
	case qty >= 10:
		total = total * 95 / 100
	}
	return total
}

// OrderTotal adds the lines and the shipping fee. Orders of 50 000 cents or more ship free.
func OrderTotal(lines []Line) int {
	sum := 0
	for _, l := range lines {
		sum += LineTotal(l.UnitCents, l.Qty)
	}
	if sum < 50000 {
		sum += 1500
	}
	return sum
}
