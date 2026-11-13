// Package orders serves orders.
package orders

// Service implements the orders API.
type Service struct{ store *store }

type store struct{ q *queries }

type queries struct{}

func (q *queries) ListOrderRows() []int          { return nil }
func (q *queries) GetCustomer(id int) int        { return id }
func (q *queries) GetOrderRow(id int) int        { return id }
func (q *queries) ListCustomersByIDs(i []int) int { return 0 }
func (q *queries) CountOrders() int              { return 0 }

// ListOrders is the classic N+1: one customer query per order row.
func (s *Service) ListOrders() {
	rows := s.store.q.ListOrderRows()
	for _, r := range rows {
		s.store.q.GetCustomer(r)
	}
}

// GetOrder runs two queries once each, through a helper and a store method.
func (s *Service) GetOrder() {
	s.store.q.GetOrderRow(1)
	s.loadCustomer()
}

func (s *Service) loadCustomer() { s.store.q.GetCustomer(1) }

// RecentOrders loads three fixed pages: a literal-bounded loop is not N.
func (s *Service) RecentOrders() {
	for i := 0; i < 3; i++ {
		s.store.q.ListOrderRows()
	}
	ids := []int{1, 2}
	s.store.q.ListCustomersByIDs(ids)
}

// OrderSummary reaches its query through the store, which has a method of the
// same name as the handler.
func (s *Service) OrderSummary() { s.store.OrderSummary() }

func (st *store) OrderSummary() { st.q.CountOrders() }
