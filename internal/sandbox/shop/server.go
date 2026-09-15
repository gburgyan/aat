package shop

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Options configures a sandbox Server.
type Options struct {
	// Latency scales the simulated delays (shipOrder 600 ms, paymentCharge
	// 350 ms). 0 disables them; 1 is the default in the CLI.
	Latency float64
	// Seed drives the token suffixes and tracking numbers. 0 means time-based.
	Seed int64
	// NoAuth disables the bearer and API-key checks.
	NoAuth bool
	// Now supplies the clock (default time.Now); tests inject a fixed clock.
	Now func() time.Time
}

// Server is the in-memory sandbox. It serves two http.Handlers: the shop API
// (catalog, carts, orders, shipments, OAuth token endpoint) and the payments
// API (charges, refunds), which the CLI binds to separate ports so that
// AAT's per-operation host overrides are load-bearing.
type Server struct {
	opts   Options
	now    func() time.Time
	seed   int64
	tokens *tokenStore
	limits *rateLimits

	mu     sync.RWMutex
	stores map[string]*store

	api http.Handler
	pay http.Handler
}

// New builds a Server. Both handlers are ready to serve immediately.
func New(opts Options) *Server {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	seed := opts.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	s := &Server{opts: opts, now: opts.Now, seed: seed}
	s.tokens = newTokenStore(seed, opts.Now)
	s.limits = &rateLimits{}
	s.resetStores()
	s.api = s.buildAPI()
	s.pay = s.buildPayments()
	return s
}

// APIHandler returns the shop API handler (default port 8765).
func (s *Server) APIHandler() http.Handler { return s.api }

// PaymentsHandler returns the payments API handler (default port 8766).
func (s *Server) PaymentsHandler() http.Handler { return s.pay }

// Reset wipes every region's data and restarts the ID sequences. Issued
// tokens stay valid.
func (s *Server) Reset() { s.resetStores() }

func (s *Server) resetStores() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stores = map[string]*store{}
	for i, name := range regionNames {
		s.stores[name] = newStore(regions[name], s.seed+int64(i))
	}
}

// store returns the region store for a request whose region path value has
// already been validated by requireRegion.
func (s *Server) store(r *http.Request) *store {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stores[r.PathValue("region")]
}

// sleep simulates latency scaled by Options.Latency, returning early when the
// request is cancelled.
func (s *Server) sleep(ctx context.Context, d time.Duration) {
	if s.opts.Latency <= 0 {
		return
	}
	t := time.NewTimer(time.Duration(float64(d) * s.opts.Latency))
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

func (s *Server) buildAPI() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler("shop-api"))
	mux.HandleFunc("POST /oauth/token", s.handleToken)
	mux.HandleFunc("POST /admin/reset", s.handleReset)
	mux.HandleFunc("/{region}/v1/payments/", s.handleWrongListener)

	v1 := http.NewServeMux()
	v1.HandleFunc("GET /{region}/v1/products", s.handleListProducts)
	v1.HandleFunc("GET /{region}/v1/inventory/{sku}", s.handleCheckInventory)
	v1.HandleFunc("POST /{region}/v1/carts", s.handleCreateCart)
	v1.HandleFunc("GET /{region}/v1/carts/{cartId}", s.handleGetCart)
	v1.HandleFunc("DELETE /{region}/v1/carts/{cartId}", s.handleDeleteCart)
	v1.HandleFunc("POST /{region}/v1/carts/{cartId}/items", s.handleAddItem)
	v1.HandleFunc("POST /{region}/v1/carts/{cartId}/coupon", s.handleApplyCoupon)
	v1.HandleFunc("POST /{region}/v1/carts/{cartId}/checkout", s.handleCheckout)
	v1.HandleFunc("GET /{region}/v1/orders/{orderId}", s.handleGetOrder)
	v1.HandleFunc("DELETE /{region}/v1/orders/{orderId}", s.handleDeleteOrder)
	v1.HandleFunc("POST /{region}/v1/orders/{orderId}/cancel", s.handleCancelOrder)
	v1.HandleFunc("POST /{region}/v1/orders/{orderId}/ship", s.handleShipOrder)
	v1.HandleFunc("POST /{region}/v1/orders/{orderId}/returns", s.handleCreateReturn)
	v1.HandleFunc("GET /{region}/v1/shipments/{shipmentId}", s.handleGetShipment)
	v1.HandleFunc("POST /{region}/v1/shipments/{shipmentId}/deliver", s.handleDeliverShipment)
	v1.HandleFunc("/{region}/v1/", handleNotFound)
	mux.Handle("/{region}/v1/", requireRegion(s.requireBearer(s.reportRateLimit(v1))))
	return mux
}

func (s *Server) buildPayments() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler("shop-payments"))

	v1 := http.NewServeMux()
	v1.HandleFunc("POST /{region}/v1/payments/charges", s.handleCharge)
	v1.HandleFunc("POST /{region}/v1/payments/refunds", s.handleRefund)
	v1.HandleFunc("/{region}/v1/", handleNotFound)
	mux.Handle("/{region}/v1/", requireRegion(s.requireAPIKey(v1)))
	return mux
}

func healthHandler(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": service, "regions": regionNames})
	}
}

func (s *Server) handleReset(w http.ResponseWriter, _ *http.Request) {
	s.Reset()
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func (s *Server) handleWrongListener(w http.ResponseWriter, r *http.Request) {
	writeError(w, newError(http.StatusNotFound, CodeWrongListener,
		"%s is served by the payments listener (default port 8766), not the shop API; "+
			"route payment* operations there", r.URL.Path))
}

func handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, newError(http.StatusNotFound, CodeNotFound, "no route for %s %s", r.Method, r.URL.Path))
}

// requireRegion rejects unknown region prefixes with 404 REGION_NOT_FOUND.
func requireRegion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := regions[r.PathValue("region")]; !ok {
			writeError(w, newError(http.StatusNotFound, CodeRegionNotFound,
				"unknown region %q; the path must start with /us/v1 or /eu/v1", r.PathValue("region")))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Banner prints the URLs, demo credentials, and chaos rules for humans
// starting the sandbox from a terminal.
func (s *Server) Banner(w io.Writer, apiAddr, payAddr string) {
	_, _ = fmt.Fprintf(w, "aat-sandbox: shop API      http://%s/{us,eu}/v1\n", apiAddr)
	_, _ = fmt.Fprintf(w, "aat-sandbox: payments API  http://%s/{us,eu}/v1   (header %s: %s)\n", payAddr, APIKeyHeader, DemoAPIKey)
	if s.opts.NoAuth {
		_, _ = fmt.Fprintf(w, "  auth:     disabled (--no-auth)\n")
	} else {
		_, _ = fmt.Fprintf(w, "  token:    POST http://%s/oauth/token  grant_type=password username=%s password=%s client_id=%s client_secret=%s\n",
			apiAddr, DemoUsername, DemoPassword, DemoClientID, DemoClientSecret)
	}
	_, _ = fmt.Fprintf(w, "  regions:  us (%s)  ·  eu (%s)\n", regions["us"].Description, regions["eu"].Description)
	_, _ = fmt.Fprintf(w, "  chaos:    GET /inventory/%s -> STALE_READ once per token; GET /shipments/{id} -> 503 for the first %d calls\n",
		StaleSKU, TrackingWarmupCalls)
	_, _ = fmt.Fprintf(w, "  latency:  x%g (shipOrder 600 ms, paymentCharge 350 ms)\n", s.opts.Latency)
	_, _ = fmt.Fprintf(w, "  reset:    POST http://%s/admin/reset\n", apiAddr)
}
