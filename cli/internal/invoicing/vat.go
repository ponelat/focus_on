package invoicing

import "math"

func roundMoney(v float64) float64 {
	return math.Round(v*100) / 100
}

// VATBreakdown splits quantity×rate into ex-VAT subtotal, VAT, and total due.
// vatPercent is 15 for "15%", not 0.15. Zero omits VAT from the invoice.
func VATBreakdown(quantity, rate, vatPercent float64, rateIncludesVAT bool) (subtotal, vat, total float64) {
	gross := quantity * rate
	if vatPercent <= 0 {
		total = roundMoney(gross)
		return total, 0, total
	}
	if rateIncludesVAT {
		total = roundMoney(gross)
		subtotal = roundMoney(total / (1 + vatPercent/100))
		vat = roundMoney(total - subtotal)
		return subtotal, vat, total
	}
	subtotal = roundMoney(gross)
	vat = roundMoney(subtotal * vatPercent / 100)
	total = roundMoney(subtotal + vat)
	return subtotal, vat, total
}
