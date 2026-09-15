package shop

import (
	"cmp"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	shipLatency    = 600 * time.Millisecond
	paymentLatency = 350 * time.Millisecond
)

func (s *Server) lookupOrder(w http.ResponseWriter, r *http.Request, st *store) (*order, bool) {
	id := r.PathValue("orderId")
	o, ok := st.orders[id]
	if !ok {
		writeError(w, notFound("order", id))
		return nil, false
	}
	return o, true
}

// handleGetOrder implements getOrder (GET /orders/{orderId}).
func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	st := s.store(r)
	st.mu.Lock()
	defer st.mu.Unlock()
	if o, ok := s.lookupOrder(w, r, st); ok {
		writeJSON(w, http.StatusOK, o)
	}
}

// orderPageJSON is one page of listOrders: the orders, and how the page was cut.
type orderPageJSON struct {
	Orders []*order      `json:"orders"`
	Meta   orderPageMeta `json:"meta"`
}

// orderPageMeta holds a page's limit and the cursor to the next page, null on
// the last one.
type orderPageMeta struct {
	Limit int     `json:"limit"`
	After *string `json:"after"`
}

// handleListOrders implements listOrders (GET /orders). Orders come in ID
// order, limit at a time (1 to 100, default 20), starting after the order the
// after cursor names, which need not exist any more; customerEmail keeps one
// customer's orders. A full page's meta.after is its last order's ID, so a
// listing whose size is a multiple of limit ends with an empty page, and a
// shorter page's is null.
func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 20
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			writeError(w, validation("limit must be a whole number from 1 to 100, not %q", v))
			return
		}
		limit = n
	}
	after, email := q.Get("after"), q.Get("customerEmail")

	st := s.store(r)
	st.mu.Lock()
	defer st.mu.Unlock()
	listed := make([]*order, 0, len(st.orders))
	for _, o := range st.orders {
		if (email == "" || o.CustomerEmail == email) && (after == "" || compareIDs(o.ID, after) > 0) {
			listed = append(listed, o)
		}
	}
	slices.SortFunc(listed, func(a, b *order) int { return compareIDs(a.ID, b.ID) })
	page := orderPageJSON{Orders: listed[:min(limit, len(listed))], Meta: orderPageMeta{Limit: limit}}
	if len(page.Orders) == limit {
		last := page.Orders[limit-1].ID
		page.Meta.After = &last
	}
	writeJSON(w, http.StatusOK, page)
}

// compareIDs orders sequential IDs such as ord_0009 and ord_10000 by number: a
// shorter ID comes first, and IDs of the same length compare as text.
func compareIDs(a, b string) int {
	if c := cmp.Compare(len(a), len(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// handleDeleteOrder implements deleteOrder (DELETE /orders/{orderId}). It
// succeeds in any state: it is the demo's cleanup hook.
func (s *Server) handleDeleteOrder(w http.ResponseWriter, r *http.Request) {
	st := s.store(r)
	st.mu.Lock()
	defer st.mu.Unlock()
	o, ok := s.lookupOrder(w, r, st)
	if !ok {
		return
	}
	delete(st.orders, o.ID)
	w.WriteHeader(http.StatusNoContent)
}

// handleCancelOrder implements cancelOrder (POST /orders/{orderId}/cancel).
func (s *Server) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	st := s.store(r)
	st.mu.Lock()
	defer st.mu.Unlock()
	o, ok := s.lookupOrder(w, r, st)
	if !ok {
		return
	}
	if e := o.transition("cancel"); e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// handleShipOrder implements shipOrder (POST /orders/{orderId}/ship). Paid
// orders only; takes ~600 ms so the archive Gantt shows a real bar.
func (s *Server) handleShipOrder(w http.ResponseWriter, r *http.Request) {
	st := s.store(r)
	st.mu.Lock()
	o, ok := s.lookupOrder(w, r, st)
	if !ok {
		st.mu.Unlock()
		return
	}
	if e := o.transitionError("ship"); e != nil {
		st.mu.Unlock()
		writeError(w, e)
		return
	}
	st.mu.Unlock()

	s.sleep(r.Context(), shipLatency)

	st.mu.Lock()
	defer st.mu.Unlock()
	o, ok = s.lookupOrder(w, r, st) // may have been deleted or moved meanwhile
	if !ok {
		return
	}
	if e := o.transition("ship"); e != nil {
		writeError(w, e)
		return
	}
	now := s.now()
	sh := &shipment{
		ID:             st.nextID("shp"),
		OrderID:        o.ID,
		OrderStatus:    o.Status,
		Status:         shipmentInTransit,
		Carrier:        "AAT Express",
		TrackingNumber: st.trackingNumber(),
		ETA:            dateOnly(now.Add(72 * time.Hour)),
		CreatedAt:      rfc3339(now),
	}
	st.shipments[sh.ID] = sh
	o.ShipmentID = sh.ID
	writeJSON(w, http.StatusCreated, sh)
}

// handleGetShipment implements getShipment (GET /shipments/{shipmentId}).
// The first TrackingWarmupCalls reads of each shipment return 503 with a
// Retry-After header, exercising transient-error retries.
func (s *Server) handleGetShipment(w http.ResponseWriter, r *http.Request) {
	st := s.store(r)
	st.mu.Lock()
	defer st.mu.Unlock()
	id := r.PathValue("shipmentId")
	sh, ok := st.shipments[id]
	if !ok {
		writeError(w, notFound("shipment", id))
		return
	}
	sh.polls++
	if sh.polls <= TrackingWarmupCalls {
		w.Header().Set("Retry-After", "1")
		writeError(w, newError(http.StatusServiceUnavailable, CodeTrackingUnavailable,
			"carrier tracking for %s is warming up (attempt %d of %d); retry shortly",
			id, sh.polls, TrackingWarmupCalls+1))
		return
	}
	writeJSON(w, http.StatusOK, sh)
}

// handleDeliverShipment implements deliverShipment
// (POST /shipments/{shipmentId}/deliver).
func (s *Server) handleDeliverShipment(w http.ResponseWriter, r *http.Request) {
	st := s.store(r)
	st.mu.Lock()
	defer st.mu.Unlock()
	id := r.PathValue("shipmentId")
	sh, ok := st.shipments[id]
	if !ok {
		writeError(w, notFound("shipment", id))
		return
	}
	o, ok := st.orders[sh.OrderID]
	if !ok {
		writeError(w, notFound("order", sh.OrderID))
		return
	}
	if e := o.transition("deliver"); e != nil {
		writeError(w, e)
		return
	}
	sh.Status = shipmentDelivered
	sh.OrderStatus = o.Status
	writeJSON(w, http.StatusOK, sh)
}

// handleCreateReturn implements createReturn (POST /orders/{orderId}/returns).
func (s *Server) handleCreateReturn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if e := decodeJSON(r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	if req.Reason == "" {
		writeError(w, validation("reason is required"))
		return
	}
	st := s.store(r)
	st.mu.Lock()
	defer st.mu.Unlock()
	o, ok := s.lookupOrder(w, r, st)
	if !ok {
		return
	}
	if e := o.transition("return"); e != nil {
		writeError(w, e)
		return
	}
	ret := &orderReturn{
		ID:          st.nextID("ret"),
		RMANumber:   st.nextNumber("RMA"),
		OrderID:     o.ID,
		OrderStatus: o.Status,
		Reason:      req.Reason,
		CreatedAt:   rfc3339(s.now()),
	}
	st.returns[ret.ID] = ret
	writeJSON(w, http.StatusCreated, ret)
}
