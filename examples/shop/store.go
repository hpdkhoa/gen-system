package shop

import (
	"database/sql"
	"errors"
)

// Line is one product on an order.
type Line struct {
	SKU       string
	UnitCents int
	Qty       int
}

// Store keeps orders and stock in a SQL database.
type Store struct {
	db *sql.DB
}

// ErrOutOfStock means a line asks for more than the warehouse holds.
var ErrOutOfStock = errors.New("out of stock")

// Reserve takes stock for every line, or for none of them.
func (s *Store) Reserve(lines []Line) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, l := range lines {
		var left int
		if err := tx.QueryRow("SELECT qty FROM stock WHERE sku = $1", l.SKU).Scan(&left); err != nil {
			tx.Rollback()
			return err
		}
		if left < l.Qty {
			tx.Rollback()
			return ErrOutOfStock
		}
		if _, err := tx.Exec("UPDATE stock SET qty = qty - $1 WHERE sku = $2", l.Qty, l.SKU); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SaveOrder writes the order and returns its id.
func (s *Store) SaveOrder(customer string, totalCents int) (int64, error) {
	var id int64
	err := s.db.QueryRow(
		"INSERT INTO orders (customer, total_cents) VALUES ($1, $2) RETURNING id",
		customer, totalCents).Scan(&id)
	return id, err
}
