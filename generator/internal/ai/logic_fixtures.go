// generator/internal/ai/logic_fixtures.go — v23.10 (Phase 4-B): deterministic
// replay fixtures. These stand in for the model offline so the full
// generate→inject→compile→test path is verifiable without Ollama. They are REAL
// business operations (not CRUD) written against the fixed signature
//
//	func (s *<E>Service) <Name>(id int, in map[string]any) (any, error)
//
// so the generated service compiles and the methods do something meaningful.
//
// On your box, GEN_LOGIC=ollama replaces these with live deepseek-coder output.
package ai

// builtinLogicFixtures maps entity name -> operations. Keyed to the worked
// "orders/checkout" example; unknown entities get no extra ops (CRUD only),
// which is a valid, build-green outcome.
func builtinLogicFixtures() map[string][]Operation {
	return map[string][]Operation{
		"Order": {
			{
				Name:       "Checkout",
				HTTPMethod: "POST",
				Subpath:    "checkout",
				Doc:        "Checkout finalizes an order: requires items, marks it paid, stamps the total.",
				Body: `o, ok := s.store.Get(id)
	if !ok {
		return nil, errors.New("order not found")
	}
	if o.Status == "paid" {
		return nil, errors.New("order already paid")
	}
	if o.Total <= 0 {
		return nil, errors.New("cannot checkout an order with no total")
	}
	o.Status = "paid"
	updated, ok := s.store.Update(id, o)
	if !ok {
		return nil, errors.New("failed to update order")
	}
	return updated, nil`,
			},
			{
				Name:       "ApplyDiscount",
				HTTPMethod: "POST",
				Subpath:    "discount",
				Doc:        "ApplyDiscount reduces an unpaid order's total by a percentage in `in[\"percent\"]`.",
				Body: `o, ok := s.store.Get(id)
	if !ok {
		return nil, errors.New("order not found")
	}
	if o.Status == "paid" {
		return nil, errors.New("cannot discount a paid order")
	}
	pctRaw, ok := in["percent"]
	if !ok {
		return nil, errors.New("percent is required")
	}
	pct, ok := pctRaw.(float64)
	if !ok || pct < 0 || pct > 100 {
		return nil, errors.New("percent must be a number between 0 and 100")
	}
	o.Total = o.Total * (1 - pct/100)
	updated, ok := s.store.Update(id, o)
	if !ok {
		return nil, errors.New("failed to update order")
	}
	return updated, nil`,
			},
		},
		"Product": {
			{
				Name:       "AdjustStock",
				HTTPMethod: "POST",
				Subpath:    "adjust-stock",
				Doc:        "AdjustStock changes a product's stock by the signed delta in `in[\"delta\"]`, refusing to go negative.",
				Body: `p, ok := s.store.Get(id)
	if !ok {
		return nil, errors.New("product not found")
	}
	deltaRaw, ok := in["delta"]
	if !ok {
		return nil, errors.New("delta is required")
	}
	delta, ok := deltaRaw.(float64)
	if !ok {
		return nil, errors.New("delta must be a number")
	}
	newStock := p.Stock + int(delta)
	if newStock < 0 {
		return nil, errors.New("stock cannot go negative")
	}
	p.Stock = newStock
	updated, ok := s.store.Update(id, p)
	if !ok {
		return nil, errors.New("failed to update product")
	}
	return updated, nil`,
			},
		},
	}
}
