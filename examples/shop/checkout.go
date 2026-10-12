package shop

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// Shop wires the store to the HTTP handlers.
type Shop struct {
	Store      *Store
	ReceiptDir string
}

type checkoutRequest struct {
	Customer string `json:"customer"`
	Lines    []Line `json:"lines"`
}

// Checkout reserves stock, saves the order and writes a receipt.
func (s *Shop) Checkout(w http.ResponseWriter, r *http.Request) {
	var req checkoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(req.Lines) == 0 {
		http.Error(w, "empty order", http.StatusBadRequest)
		return
	}
	if err := s.Store.Reserve(req.Lines); err != nil {
		if err == ErrOutOfStock {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	total := OrderTotal(req.Lines)
	id, err := s.Store.SaveOrder(req.Customer, total)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.writeReceipt(id, req.Customer, total); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"id": id, "total_cents": total})
}

func (s *Shop) writeReceipt(id int64, customer string, totalCents int) error {
	text := fmt.Sprintf("order %d\ncustomer %s\ntotal %d.%02d\n", id, customer, totalCents/100, totalCents%100)
	return os.WriteFile(filepath.Join(s.ReceiptDir, fmt.Sprintf("%d.txt", id)), []byte(text), 0o644)
}
