package shop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedNow() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }

// env runs both listeners behind the contract validator.
type env struct {
	t     *testing.T
	srv   *Server
	api   *httptest.Server
	pay   *httptest.Server
	token string
}

type resp struct {
	Status int
	Header http.Header
	Body   map[string]any
	Raw    []byte
}

func (r resp) str(key string) string {
	v, _ := r.Body[key].(string)
	return v
}

func (r resp) num(key string) int64 {
	v, _ := r.Body[key].(float64)
	return int64(v)
}

func (r resp) errCode() string {
	e, _ := r.Body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

// with returns a copy of the env whose helpers report against t (for subtests).
func (e *env) with(t *testing.T) *env {
	c := *e
	c.t = t
	return &c
}

func newEnv(t *testing.T, opts Options) *env {
	t.Helper()
	if opts.Now == nil {
		opts.Now = fixedNow
	}
	if opts.Seed == 0 {
		opts.Seed = 1
	}
	c := loadContract(t)
	s := New(opts)
	api := httptest.NewServer(c.wrap(t, s.APIHandler()))
	pay := httptest.NewServer(c.wrap(t, s.PaymentsHandler()))
	t.Cleanup(func() {
		api.Close()
		pay.Close()
	})
	e := &env{t: t, srv: s, api: api, pay: pay}
	if !opts.NoAuth {
		e.token = e.newToken("password")
	}
	return e
}

func (e *env) newToken(grant string) string {
	e.t.Helper()
	form := url.Values{
		"grant_type":    {grant},
		"username":      {DemoUsername},
		"password":      {DemoPassword},
		"client_id":     {DemoClientID},
		"client_secret": {DemoClientSecret},
	}
	r := e.tokenRequest(form)
	require.Equalf(e.t, http.StatusOK, r.Status, "token: %s", r.Raw)
	return r.str("access_token")
}

func (e *env) tokenRequest(form url.Values) resp {
	e.t.Helper()
	return e.do(http.MethodPost, e.api.URL+"/oauth/token", form.Encode(),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
}

// do issues a request. body may be nil, a raw string, or a value to encode
// as JSON.
func (e *env) do(method, rawURL string, body any, headers map[string]string) resp {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		data, err := json.Marshal(b)
		require.NoError(e.t, err)
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, rawURL, rd)
	require.NoError(e.t, err)
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(e.t, err)
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	require.NoError(e.t, err)
	out := resp{Status: res.StatusCode, Header: res.Header, Raw: raw}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out.Body)
	}
	return out
}

func (e *env) shop(method, path string, body any) resp {
	e.t.Helper()
	return e.do(method, e.api.URL+path, body, map[string]string{"Authorization": "Bearer " + e.token})
}

func (e *env) payments(method, path string, body any) resp {
	e.t.Helper()
	return e.do(method, e.pay.URL+path, body, map[string]string{APIKeyHeader: DemoAPIKey})
}

func (e *env) must(r resp, status int) resp {
	e.t.Helper()
	require.Equalf(e.t, status, r.Status, "unexpected status; body: %s", r.Raw)
	return r
}

// Flow helpers --------------------------------------------------------------

func (e *env) newCart(region string) string {
	e.t.Helper()
	return e.must(e.shop("POST", "/"+region+"/v1/carts", nil), 201).str("cartId")
}

func (e *env) addItem(region, cartID, sku string, qty int) resp {
	e.t.Helper()
	return e.shop("POST", "/"+region+"/v1/carts/"+cartID+"/items", map[string]any{"sku": sku, "quantity": qty})
}

func (e *env) checkout(region, cartID, tier string) resp {
	e.t.Helper()
	return e.shop("POST", "/"+region+"/v1/carts/"+cartID+"/checkout",
		map[string]any{"shippingTier": tier, "postalCode": "94110"})
}

// createOrder builds a one-line order (SKU-1001 x1, standard shipping).
func (e *env) createOrder(region string) resp {
	e.t.Helper()
	cartID := e.newCart(region)
	e.must(e.addItem(region, cartID, "SKU-1001", 1), 201)
	return e.must(e.checkout(region, cartID, "standard"), 201)
}

func (e *env) charge(region string, o resp, method string, extra map[string]any) resp {
	e.t.Helper()
	body := map[string]any{
		"orderId":  o.str("orderId"),
		"amount":   o.num("total"),
		"currency": o.str("currency"),
		"method":   method,
	}
	switch method {
	case methodCard:
		body["cardNumber"] = "4111111111111111"
	case methodGiftCard:
		body["giftCardCode"] = "GC-500-DEMO"
	case methodPayPal:
		body["paypalEmail"] = "demo@example.com"
	}
	for k, v := range extra {
		body[k] = v
	}
	return e.payments("POST", "/"+region+"/v1/payments/charges", body)
}

func (e *env) getOrder(region, id string) resp {
	e.t.Helper()
	return e.must(e.shop("GET", "/"+region+"/v1/orders/"+id, nil), 200)
}

// orderIn drives a fresh order into the requested state.
func (e *env) orderIn(region, state string) resp {
	e.t.Helper()
	o := e.createOrder(region)
	id := o.str("orderId")
	if state == orderCreated {
		return o
	}
	if state == orderCancelled {
		return e.must(e.shop("POST", "/"+region+"/v1/orders/"+id+"/cancel", nil), 200)
	}
	e.must(e.charge(region, o, methodCard, nil), 201)
	if state == orderPaid {
		return e.getOrder(region, id)
	}
	sh := e.must(e.shop("POST", "/"+region+"/v1/orders/"+id+"/ship", nil), 201)
	if state == orderShipped {
		return e.getOrder(region, id)
	}
	e.must(e.shop("POST", "/"+region+"/v1/shipments/"+sh.str("shipmentId")+"/deliver", nil), 200)
	if state == orderDelivered {
		return e.getOrder(region, id)
	}
	e.must(e.shop("POST", "/"+region+"/v1/orders/"+id+"/returns", map[string]any{"reason": "changed my mind"}), 201)
	require.Equal(e.t, orderReturned, state, "unknown target state")
	return e.getOrder(region, id)
}

// Tests ----------------------------------------------------------------------

func TestHealth(t *testing.T) {
	e := newEnv(t, Options{})
	for _, base := range []string{e.api.URL, e.pay.URL} {
		r := e.do("GET", base+"/healthz", nil, nil)
		assert.Equal(t, 200, r.Status)
		assert.Equal(t, "ok", r.str("status"))
	}
}

func TestToken_Grants(t *testing.T) {
	e := newEnv(t, Options{})
	base := url.Values{"client_id": {DemoClientID}, "client_secret": {DemoClientSecret}}

	t.Run("password", func(t *testing.T) {
		e := e.with(t)
		f := url.Values{}
		for k, v := range base {
			f[k] = v
		}
		f.Set("grant_type", "password")
		f.Set("username", DemoUsername)
		f.Set("password", DemoPassword)
		r := e.tokenRequest(f)
		require.Equal(t, 200, r.Status)
		assert.Equal(t, "Bearer", r.str("token_type"))
		assert.EqualValues(t, tokenTTLSeconds, r.num("expires_in"))
		assert.True(t, strings.HasPrefix(r.str("access_token"), "shop-"))
	})
	t.Run("client_credentials", func(t *testing.T) {
		e := e.with(t)
		f := url.Values{}
		for k, v := range base {
			f[k] = v
		}
		f.Set("grant_type", "client_credentials")
		assert.Equal(t, 200, e.tokenRequest(f).Status)
	})
	t.Run("basic auth client", func(t *testing.T) {
		e := e.with(t)
		req, _ := http.NewRequest("POST", e.api.URL+"/oauth/token", strings.NewReader("grant_type=client_credentials"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(DemoClientID, DemoClientSecret)
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		assert.Equal(t, 200, res.StatusCode)
	})
	t.Run("bad password", func(t *testing.T) {
		e := e.with(t)
		f := url.Values{}
		for k, v := range base {
			f[k] = v
		}
		f.Set("grant_type", "password")
		f.Set("username", DemoUsername)
		f.Set("password", "wrong")
		r := e.tokenRequest(f)
		assert.Equal(t, 400, r.Status)
		assert.Equal(t, "invalid_grant", r.str("error"))
	})
	t.Run("bad client", func(t *testing.T) {
		e := e.with(t)
		f := url.Values{"grant_type": {"client_credentials"}, "client_id": {"nope"}, "client_secret": {"nope"}}
		r := e.tokenRequest(f)
		assert.Equal(t, 401, r.Status)
		assert.Equal(t, "invalid_client", r.str("error"))
	})
	t.Run("unsupported grant", func(t *testing.T) {
		e := e.with(t)
		f := url.Values{}
		for k, v := range base {
			f[k] = v
		}
		f.Set("grant_type", "authorization_code")
		r := e.tokenRequest(f)
		assert.Equal(t, 400, r.Status)
		assert.Equal(t, "unsupported_grant_type", r.str("error"))
	})
}

func TestAuth_Required(t *testing.T) {
	e := newEnv(t, Options{})
	t.Run("missing bearer", func(t *testing.T) {
		e := e.with(t)
		r := e.do("GET", e.api.URL+"/us/v1/products", nil, nil)
		assert.Equal(t, 401, r.Status)
		assert.Equal(t, CodeUnauthorized, r.errCode())
		assert.Contains(t, r.Header.Get("WWW-Authenticate"), "Bearer")
	})
	t.Run("bad bearer", func(t *testing.T) {
		e := e.with(t)
		r := e.do("GET", e.api.URL+"/us/v1/products", nil, map[string]string{"Authorization": "Bearer nope"})
		assert.Equal(t, 401, r.Status)
	})
	t.Run("payments without key", func(t *testing.T) {
		e := e.with(t)
		r := e.do("POST", e.pay.URL+"/us/v1/payments/charges", map[string]any{"orderId": "x"}, nil)
		assert.Equal(t, 401, r.Status)
		assert.Equal(t, CodeUnauthorized, r.errCode())
	})
	t.Run("payments with wrong key", func(t *testing.T) {
		e := e.with(t)
		r := e.do("POST", e.pay.URL+"/us/v1/payments/refunds", map[string]any{"orderId": "x"}, map[string]string{APIKeyHeader: "nope"})
		assert.Equal(t, 401, r.Status)
	})
	t.Run("payments path on the shop listener", func(t *testing.T) {
		e := e.with(t)
		r := e.shop("POST", "/us/v1/payments/charges", map[string]any{"orderId": "x"})
		assert.Equal(t, 404, r.Status)
		assert.Equal(t, CodeWrongListener, r.errCode())
	})
}

func TestAuth_Disabled(t *testing.T) {
	e := newEnv(t, Options{NoAuth: true})
	assert.Equal(t, 200, e.do("GET", e.api.URL+"/us/v1/products", nil, nil).Status)
	o := e.createOrder("us")
	r := e.do("POST", e.pay.URL+"/us/v1/payments/charges", map[string]any{
		"orderId": o.str("orderId"), "amount": o.num("total"), "currency": "USD", "method": "paypal", "paypalEmail": "a@b.c",
	}, nil)
	assert.Equal(t, 201, r.Status, string(r.Raw))
}

func TestRegions(t *testing.T) {
	e := newEnv(t, Options{})
	r := e.shop("GET", "/xx/v1/products", nil)
	assert.Equal(t, 404, r.Status)
	assert.Equal(t, CodeRegionNotFound, r.errCode())

	r = e.shop("GET", "/us/v1/nope", nil)
	assert.Equal(t, 404, r.Status)
	assert.Equal(t, CodeNotFound, r.errCode())

	// Regions have independent stores and sequences.
	assert.Equal(t, "cart_0001", e.newCart("us"))
	assert.Equal(t, "cart_0002", e.newCart("us"))
	assert.Equal(t, "cart_0001", e.newCart("eu"))
}

func TestListProducts(t *testing.T) {
	e := newEnv(t, Options{})
	r := e.must(e.shop("GET", "/us/v1/products", nil), 200)
	products := r.Body["products"].([]any)
	require.Len(t, products, 6)
	assert.Equal(t, "USD", r.str("currency"))
	var outOfStock []string
	for _, p := range products {
		m := p.(map[string]any)
		if m["inStock"] == false {
			outOfStock = append(outOfStock, m["sku"].(string))
		}
	}
	assert.Equal(t, []string{"SKU-1005"}, outOfStock)

	r = e.must(e.shop("GET", "/us/v1/products?category=gear", nil), 200)
	assert.Len(t, r.Body["products"], 2)

	r = e.must(e.shop("GET", "/eu/v1/products?category=gear", nil), 200)
	first := r.Body["products"].([]any)[0].(map[string]any)
	assert.Equal(t, "EUR", first["currency"])
	assert.EqualValues(t, 8279, first["price"]) // 8999 * 0.92
	assert.Equal(t, "€82.79", first["priceDisplay"])
}

func TestListOrders(t *testing.T) {
	e := newEnv(t, Options{})
	orderFor := func(email string) string {
		cartID := e.must(e.shop("POST", "/us/v1/carts", map[string]any{"customerEmail": email}), 201).str("cartId")
		e.must(e.addItem("us", cartID, "SKU-1001", 1), 201)
		return e.must(e.checkout("us", cartID, "standard"), 201).str("orderId")
	}
	a1, a2, a3 := orderFor("a@example.com"), orderFor("a@example.com"), orderFor("a@example.com")
	b1 := orderFor("b@example.com")

	tests := []struct {
		name  string
		query string
		want  []string
		limit int
		after any // the next page's cursor, nil on the last page
	}{
		{name: "a full page gives its last order as the cursor", query: "limit=2&customerEmail=a@example.com", want: []string{a1, a2}, limit: 2, after: a2},
		{name: "a shorter page is the last", query: "limit=2&customerEmail=a@example.com&after=" + a2, want: []string{a3}, limit: 2},
		{name: "a full page still gives a cursor when nothing follows", query: "limit=1&customerEmail=b@example.com", want: []string{b1}, limit: 1, after: b1},
		{name: "which leads to an empty last page", query: "limit=1&customerEmail=b@example.com&after=" + b1, want: []string{}, limit: 1},
		{name: "every customer, with the default limit", query: "", want: []string{a1, a2, a3, b1}, limit: 20},
		{name: "a customer with no orders", query: "customerEmail=nobody@example.com", want: []string{}, limit: 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.must(e.shop("GET", "/us/v1/orders?"+tt.query, nil), 200)
			ids := []string{}
			for _, o := range r.Body["orders"].([]any) {
				ids = append(ids, o.(map[string]any)["orderId"].(string))
			}
			assert.Equal(t, tt.want, ids)
			meta := r.Body["meta"].(map[string]any)
			assert.EqualValues(t, tt.limit, meta["limit"])
			assert.Equal(t, tt.after, meta["after"])
		})
	}

	t.Run("a deleted order still works as the cursor", func(t *testing.T) {
		e.must(e.shop("DELETE", "/us/v1/orders/"+a2, nil), 204)
		r := e.must(e.shop("GET", "/us/v1/orders?customerEmail=a@example.com&after="+a2, nil), 200)
		require.Len(t, r.Body["orders"], 1)
		assert.Equal(t, a3, r.Body["orders"].([]any)[0].(map[string]any)["orderId"])
	})

	for _, limit := range []string{"0", "101", "x"} {
		t.Run("limit "+limit+" is refused", func(t *testing.T) {
			r := e.must(e.shop("GET", "/us/v1/orders?limit="+limit, nil), 400)
			assert.Equal(t, CodeValidation, r.errCode())
		})
	}
}

func TestCompareIDs(t *testing.T) {
	assert.Negative(t, compareIDs("ord_0009", "ord_0010"))
	assert.Negative(t, compareIDs("ord_9999", "ord_10000"), "a longer sequence number comes later")
	assert.Zero(t, compareIDs("ord_0001", "ord_0001"))
}

func TestCheckInventory_StaleReadOncePerToken(t *testing.T) {
	e := newEnv(t, Options{})
	r := e.must(e.shop("GET", "/us/v1/inventory/SKU-1004", nil), 200)
	assert.Equal(t, "ERROR", r.str("status"))
	assert.Equal(t, "STALE_READ", r.str("errorCode"))

	r = e.must(e.shop("GET", "/us/v1/inventory/SKU-1004", nil), 200)
	assert.Equal(t, "OK", r.str("status"))
	assert.EqualValues(t, 9, r.num("available"))

	// Other SKUs never go stale.
	r = e.must(e.shop("GET", "/us/v1/inventory/SKU-1001", nil), 200)
	assert.Equal(t, "OK", r.str("status"))

	// A fresh session sees the stale read again.
	e.token = e.newToken("password")
	r = e.must(e.shop("GET", "/us/v1/inventory/SKU-1004", nil), 200)
	assert.Equal(t, "ERROR", r.str("status"))

	r = e.shop("GET", "/us/v1/inventory/SKU-9999", nil)
	assert.Equal(t, 404, r.Status)
}

func TestCreateCart(t *testing.T) {
	e := newEnv(t, Options{})
	r := e.must(e.shop("POST", "/us/v1/carts", map[string]any{"customerEmail": "demo@example.com"}), 201)
	assert.Equal(t, "demo@example.com", r.str("customerEmail"))
	assert.Equal(t, "open", r.str("status"))
	assert.EqualValues(t, 0, r.num("lineCount"))

	r = e.shop("POST", "/us/v1/carts", map[string]any{"customerEmail": "not-an-email"})
	assert.Equal(t, 400, r.Status)
	assert.Equal(t, CodeValidation, r.errCode())
}

func TestAddItem_ErrorOrdering(t *testing.T) {
	e := newEnv(t, Options{})
	open := e.newCart("us")
	closed := e.newCart("us")
	e.must(e.addItem("us", closed, "SKU-1001", 1), 201)
	e.must(e.checkout("us", closed, "standard"), 201)

	cases := []struct {
		name   string
		cart   string
		body   any
		status int
		code   string
	}{
		{"malformed body", open, `{"sku": "SKU-1001", "quantity": `, 400, CodeValidation},
		{"wrong type", open, `{"sku": "SKU-1001", "quantity": "two"}`, 400, CodeValidation},
		{"missing sku", open, map[string]any{"quantity": 1}, 400, CodeValidation},
		{"zero quantity", open, map[string]any{"sku": "SKU-1001", "quantity": 0}, 400, CodeValidation},
		{"validation beats unknown cart", "cart_9999", map[string]any{"sku": "SKU-1001", "quantity": 0}, 400, CodeValidation},
		{"unknown cart", "cart_9999", map[string]any{"sku": "SKU-1001", "quantity": 1}, 404, CodeNotFound},
		{"checked-out cart", closed, map[string]any{"sku": "SKU-1001", "quantity": 1}, 409, CodeCartNotOpen},
		{"unknown sku", open, map[string]any{"sku": "SKU-9999", "quantity": 1}, 404, CodeNotFound},
		{"out of stock", open, map[string]any{"sku": "SKU-1005", "quantity": 1}, 409, CodeOutOfStock},
		{"more than in stock", open, map[string]any{"sku": "SKU-1004", "quantity": 10}, 409, CodeOutOfStock},
		{"happy path", open, map[string]any{"sku": "SKU-1001", "quantity": 2}, 201, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := e.with(t)
			r := e.shop("POST", "/us/v1/carts/"+tc.cart+"/items", tc.body)
			assert.Equal(t, tc.status, r.Status, string(r.Raw))
			if tc.code != "" {
				assert.Equal(t, tc.code, r.errCode())
			}
		})
	}

	// Adding the same SKU again merges quantities.
	r := e.must(e.addItem("us", open, "SKU-1001", 1), 201)
	assert.EqualValues(t, 1, r.num("lineCount"))
	assert.EqualValues(t, 3*8999, r.num("subtotal"))

	// The stock limit counts what the cart already holds (SKU-1001 has 42).
	r = e.addItem("us", open, "SKU-1001", 40)
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, CodeOutOfStock, r.errCode())
	e.must(e.addItem("us", open, "SKU-1001", 39), 201)
}

func TestGetCart_LinesAndProducts(t *testing.T) {
	e := newEnv(t, Options{})
	id := e.newCart("us")
	e.must(e.addItem("us", id, "SKU-1001", 1), 201)
	e.must(e.addItem("us", id, "SKU-1006", 3), 201)
	r := e.must(e.shop("GET", "/us/v1/carts/"+id, nil), 200)
	lines := r.Body["lines"].([]any)
	products := r.Body["products"].([]any)
	require.Len(t, lines, 2)
	require.Len(t, products, 2)
	assert.Equal(t, map[string]any{"sku": "SKU-1006", "quantity": float64(3)}, lines[1])
	assert.Equal(t, "Wool Socks", products[1].(map[string]any)["name"])
	assert.EqualValues(t, 8999+3*1899, r.num("subtotal"))

	assert.Equal(t, 404, e.shop("GET", "/us/v1/carts/cart_9999", nil).Status)
	assert.Equal(t, 204, e.shop("DELETE", "/us/v1/carts/"+id, nil).Status)
	assert.Equal(t, 404, e.shop("DELETE", "/us/v1/carts/"+id, nil).Status)
}

func TestCoupons(t *testing.T) {
	e := newEnv(t, Options{})
	coupon := func(region, cart, code string) resp {
		return e.shop("POST", "/"+region+"/v1/carts/"+cart+"/coupon", map[string]any{"code": code})
	}

	us := e.newCart("us")
	e.must(e.addItem("us", us, "SKU-1001", 1), 201)
	r := e.must(coupon("us", us, "save10"), 200) // case-insensitive
	assert.Equal(t, "SAVE10", r.str("couponCode"))
	assert.EqualValues(t, 900, r.num("discount"))

	r = coupon("us", us, "NOPE")
	assert.Equal(t, 404, r.Status)
	assert.Equal(t, CodeCouponInvalid, r.errCode())

	r = coupon("us", us, "EU-ONLY")
	assert.Equal(t, 422, r.Status)
	assert.Equal(t, CodeCouponWrongRegion, r.errCode())

	eu := e.newCart("eu")
	e.must(e.addItem("eu", eu, "SKU-1001", 1), 201)
	e.must(coupon("eu", eu, "EU-ONLY"), 200)

	// FREESHIP zeroes shipping at checkout; SAVE10 reduces the taxable base.
	free := e.newCart("us")
	e.must(e.addItem("us", free, "SKU-1001", 1), 201)
	e.must(coupon("us", free, "FREESHIP"), 200)
	o := e.must(e.checkout("us", free, "express"), 201)
	assert.EqualValues(t, 0, o.num("shipping"))
	assert.Equal(t, "FREESHIP", o.str("couponCode"))

	o = e.must(e.checkout("us", us, "standard"), 201)
	assert.EqualValues(t, 900, o.num("discount"))
	assert.EqualValues(t, 8999-900+599+668, o.num("total")) // tax = round(8099 * 0.0825) = 668

	r = coupon("us", us, "SAVE10")
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, CodeCartNotOpen, r.errCode())
}

func TestCheckout_RegionPricing(t *testing.T) {
	e := newEnv(t, Options{})

	t.Run("us adds sales tax", func(t *testing.T) {
		e := e.with(t)
		o := e.createOrder("us")
		assert.Equal(t, "ord_0001", o.str("orderId"))
		assert.Equal(t, "RCPT-US-0001", o.str("receiptNumber"))
		assert.Equal(t, "created", o.str("status"))
		assert.Equal(t, "unpaid", o.str("paymentStatus"))
		assert.EqualValues(t, 8999, o.num("subtotal"))
		assert.EqualValues(t, 599, o.num("shipping"))
		assert.EqualValues(t, 742, o.num("tax"))
		assert.EqualValues(t, 8999+599+742, o.num("total"))
		assert.Equal(t, "$103.40", o.str("totalDisplay"))
		assert.Equal(t, "Sales tax 8.25%", o.str("taxLabel"))
	})

	t.Run("eu includes vat", func(t *testing.T) {
		e := e.with(t)
		o := e.createOrder("eu")
		assert.Equal(t, "RCPT-EU-0001", o.str("receiptNumber"))
		assert.Equal(t, "EUR", o.str("currency"))
		assert.EqualValues(t, 8279, o.num("subtotal"))
		assert.EqualValues(t, 499, o.num("shipping"))
		assert.EqualValues(t, 1380, o.num("tax")) // included portion of 8279 at 20%
		assert.EqualValues(t, 8279+499, o.num("total"))
		assert.Equal(t, "€87.78", o.str("totalDisplay"))
		assert.Equal(t, "VAT 20% (included)", o.str("taxLabel"))
	})

	t.Run("eu has no overnight tier", func(t *testing.T) {
		e := e.with(t)
		c := e.newCart("eu")
		e.must(e.addItem("eu", c, "SKU-1003", 1), 201)
		r := e.checkout("eu", c, "overnight")
		assert.Equal(t, 422, r.Status)
		assert.Equal(t, CodeTierNotAvailable, r.errCode())
		e.must(e.checkout("eu", c, "express"), 201)
	})

	t.Run("validation and state", func(t *testing.T) {
		e := e.with(t)
		c := e.newCart("us")
		post := func(body any) resp { return e.shop("POST", "/us/v1/carts/"+c+"/checkout", body) }

		r := post(map[string]any{"shippingTier": "standard"})
		assert.Equal(t, 400, r.Status)
		r = post(map[string]any{"shippingTier": "drone", "postalCode": "94110"})
		assert.Equal(t, 400, r.Status)
		r = post(map[string]any{"shippingTier": "standard", "postalCode": "94110", "deliveryDate": "tomorrow"})
		assert.Equal(t, 400, r.Status)
		r = post(map[string]any{"shippingTier": "standard", "postalCode": "94110"})
		assert.Equal(t, 409, r.Status)
		assert.Equal(t, CodeCartEmpty, r.errCode())

		e.must(e.addItem("us", c, "SKU-1002", 2), 201)
		r = post(map[string]any{"shippingTier": "overnight", "postalCode": "94110", "deliveryDate": "2026-09-20",
			"customerEmail": "reg@example.com", "notes": "leave at door"})
		require.Equal(t, 201, r.Status, string(r.Raw))
		assert.Equal(t, "2026-09-20", r.str("deliveryDate"))
		assert.Equal(t, "reg@example.com", r.str("customerEmail"))
		assert.Equal(t, "overnight", r.str("shippingTier"))
		assert.EqualValues(t, 2*4599, r.num("subtotal"))

		cart := e.must(e.shop("GET", "/us/v1/carts/"+c, nil), 200)
		assert.Equal(t, "checked_out", cart.str("status"))
		r = post(map[string]any{"shippingTier": "standard", "postalCode": "94110"})
		assert.Equal(t, 409, r.Status)
		assert.Equal(t, CodeCartNotOpen, r.errCode())
	})
}

func TestOrderStateMachine(t *testing.T) {
	e := newEnv(t, Options{})
	states := []string{orderCreated, orderPaid, orderShipped, orderDelivered, orderReturned, orderCancelled}
	// action -> expected status per state
	expect := map[string]map[string]int{
		"pay":    {orderCreated: 201, orderPaid: 409, orderShipped: 409, orderDelivered: 409, orderReturned: 409, orderCancelled: 409},
		"cancel": {orderCreated: 200, orderPaid: 200, orderShipped: 409, orderDelivered: 409, orderReturned: 409, orderCancelled: 409},
		"ship":   {orderCreated: 409, orderPaid: 201, orderShipped: 409, orderDelivered: 409, orderReturned: 409, orderCancelled: 409},
		"return": {orderCreated: 409, orderPaid: 409, orderShipped: 409, orderDelivered: 201, orderReturned: 409, orderCancelled: 409},
		"refund": {orderCreated: 409, orderPaid: 201, orderShipped: 201, orderDelivered: 201, orderReturned: 201, orderCancelled: 409},
	}
	for _, state := range states {
		for action, byState := range expect {
			t.Run(state+"/"+action, func(t *testing.T) {
				e := e.with(t)
				o := e.orderIn("us", state)
				id := o.str("orderId")
				var r resp
				switch action {
				case "pay":
					r = e.charge("us", o, methodCard, nil)
				case "cancel":
					r = e.shop("POST", "/us/v1/orders/"+id+"/cancel", nil)
				case "ship":
					r = e.shop("POST", "/us/v1/orders/"+id+"/ship", nil)
				case "return":
					r = e.shop("POST", "/us/v1/orders/"+id+"/returns", map[string]any{"reason": "test"})
				case "refund":
					r = e.payments("POST", "/us/v1/payments/refunds", map[string]any{"orderId": id})
				}
				assert.Equal(t, byState[state], r.Status, string(r.Raw))
				if r.Status == 409 {
					code := r.errCode()
					assert.Contains(t, []string{CodeInvalidTransition, CodePaymentNotRefundable}, code)
				}
			})
		}
	}

	t.Run("cancel after payment stays refundable", func(t *testing.T) {
		e := e.with(t)
		o := e.orderIn("us", orderPaid)
		id := o.str("orderId")
		e.must(e.shop("POST", "/us/v1/orders/"+id+"/cancel", nil), 200)
		r := e.must(e.payments("POST", "/us/v1/payments/refunds", map[string]any{"orderId": id}), 201)
		assert.Equal(t, "refunded", r.str("status"))
		assert.Equal(t, "refunded", e.getOrder("us", id).str("paymentStatus"))
	})
}

func TestShipmentTracking(t *testing.T) {
	e := newEnv(t, Options{})
	o := e.orderIn("us", orderPaid)
	id := o.str("orderId")
	sh := e.must(e.shop("POST", "/us/v1/orders/"+id+"/ship", nil), 201)
	shID := sh.str("shipmentId")
	assert.Equal(t, "shp_0001", shID)
	assert.Equal(t, "in_transit", sh.str("status"))
	assert.Equal(t, "shipped", sh.str("orderStatus"))
	assert.Equal(t, "2026-09-13", sh.str("eta"))
	assert.Equal(t, shID, e.getOrder("us", id).str("shipmentId"))

	for i := 1; i <= TrackingWarmupCalls; i++ {
		r := e.shop("GET", "/us/v1/shipments/"+shID, nil)
		assert.Equal(t, 503, r.Status, "call %d", i)
		assert.Equal(t, CodeTrackingUnavailable, r.errCode())
		assert.Equal(t, "1", r.Header.Get("Retry-After"))
	}
	r := e.must(e.shop("GET", "/us/v1/shipments/"+shID, nil), 200)
	assert.Equal(t, "in_transit", r.str("status"))
	assert.NotEmpty(t, r.str("trackingNumber"))

	r = e.must(e.shop("POST", "/us/v1/shipments/"+shID+"/deliver", nil), 200)
	assert.Equal(t, "delivered", r.str("status"))
	assert.Equal(t, "delivered", r.str("orderStatus"))
	assert.Equal(t, "delivered", e.getOrder("us", id).str("status"))

	r = e.shop("POST", "/us/v1/shipments/"+shID+"/deliver", nil)
	assert.Equal(t, 409, r.Status)

	r = e.must(e.shop("POST", "/us/v1/orders/"+id+"/returns", map[string]any{"reason": "too big"}), 201)
	assert.Equal(t, "RMA-US-0001", r.str("rmaNumber"))
	assert.Equal(t, "returned", r.str("orderStatus"))

	r = e.shop("POST", "/us/v1/orders/"+id+"/returns", map[string]any{})
	assert.Equal(t, 400, r.Status)

	assert.Equal(t, 404, e.shop("GET", "/us/v1/shipments/shp_9999", nil).Status)
	assert.Equal(t, 404, e.shop("POST", "/us/v1/shipments/shp_9999/deliver", nil).Status)
}

func TestPayments(t *testing.T) {
	e := newEnv(t, Options{})

	t.Run("amount and currency must match", func(t *testing.T) {
		e := e.with(t)
		o := e.createOrder("us")
		r := e.charge("us", o, methodCard, map[string]any{"amount": o.num("total") - 1})
		assert.Equal(t, 422, r.Status)
		assert.Equal(t, CodeAmountMismatch, r.errCode())
		r = e.charge("us", o, methodCard, map[string]any{"currency": "EUR"})
		assert.Equal(t, 422, r.Status)
		assert.Equal(t, CodeCurrencyMismatch, r.errCode())
		r = e.charge("us", o, methodCard, map[string]any{"orderId": "ord_9999"})
		assert.Equal(t, 404, r.Status)
	})

	t.Run("request validation", func(t *testing.T) {
		e := e.with(t)
		o := e.createOrder("us")
		cases := []struct {
			name  string
			extra map[string]any
		}{
			{"unknown method", map[string]any{"method": "crypto"}},
			{"card without number", map[string]any{"cardNumber": ""}},
			{"zero amount", map[string]any{"amount": 0}},
			{"missing currency", map[string]any{"currency": ""}},
		}
		for _, tc := range cases {
			r := e.charge("us", o, methodCard, tc.extra)
			assert.Equal(t, 400, r.Status, tc.name)
			assert.Equal(t, CodeValidation, r.errCode(), tc.name)
		}
		r := e.payments("POST", "/us/v1/payments/charges", map[string]any{
			"orderId": o.str("orderId"), "amount": o.num("total"), "currency": "USD", "method": "paypal"})
		assert.Equal(t, 400, r.Status)
		r = e.payments("POST", "/us/v1/payments/charges", `{"orderId": `)
		assert.Equal(t, 400, r.Status)
	})

	t.Run("declined card leaves the order unpaid", func(t *testing.T) {
		e := e.with(t)
		o := e.createOrder("us")
		r := e.charge("us", o, methodCard, map[string]any{"cardNumber": DeclinedCard})
		assert.Equal(t, 402, r.Status)
		assert.Equal(t, CodeCardDeclined, r.errCode())
		got := e.getOrder("us", o.str("orderId"))
		assert.Equal(t, "created", got.str("status"))
		assert.Equal(t, "unpaid", got.str("paymentStatus"))
		// A good card still works afterwards.
		e.must(e.charge("us", o, methodCard, nil), 201)
	})

	t.Run("gift cards", func(t *testing.T) {
		e := e.with(t)
		o := e.createOrder("us")
		r := e.charge("us", o, methodGiftCard, map[string]any{"giftCardCode": "GC-10-DEMO"})
		assert.Equal(t, 402, r.Status)
		assert.Equal(t, CodeInsufficientFunds, r.errCode())
		r = e.charge("us", o, methodGiftCard, map[string]any{"giftCardCode": "GC-100-DEMO"})
		assert.Equal(t, 402, r.Status, "a $100 card cannot cover %s", o.str("totalDisplay"))
		r = e.charge("us", o, methodGiftCard, map[string]any{"giftCardCode": "GC-NOPE"})
		assert.Equal(t, 404, r.Status)
		assert.Equal(t, CodeGiftCardInvalid, r.errCode())
		r = e.must(e.charge("us", o, methodGiftCard, map[string]any{"giftCardCode": "GC-500-DEMO"}), 201)
		assert.Equal(t, "gift_card", r.str("method"))

		// A cheap order fits on the $100 card.
		c := e.newCart("us")
		e.must(e.addItem("us", c, "SKU-1006", 1), 201)
		small := e.must(e.checkout("us", c, "standard"), 201)
		e.must(e.charge("us", small, methodGiftCard, map[string]any{"giftCardCode": "GC-100-DEMO"}), 201)
	})

	t.Run("capture then refund", func(t *testing.T) {
		e := e.with(t)
		o := e.createOrder("us")
		id := o.str("orderId")
		p := e.must(e.charge("us", o, methodPayPal, nil), 201)
		assert.Equal(t, "captured", p.str("status"))
		assert.Equal(t, "paid", p.str("orderStatus"))
		assert.Equal(t, o.str("totalDisplay"), p.str("amountDisplay"))
		got := e.getOrder("us", id)
		assert.Equal(t, "paid", got.str("status"))
		assert.Equal(t, "captured", got.str("paymentStatus"))
		assert.Equal(t, p.str("paymentId"), got.str("paymentId"))

		r := e.charge("us", o, methodPayPal, nil)
		assert.Equal(t, 409, r.Status)
		assert.Equal(t, CodeInvalidTransition, r.errCode())

		r = e.payments("POST", "/us/v1/payments/refunds", map[string]any{"orderId": id, "amount": o.num("total") + 1})
		assert.Equal(t, 422, r.Status)
		assert.Equal(t, CodeAmountMismatch, r.errCode())

		for _, bad := range []int64{0, -5} {
			r = e.payments("POST", "/us/v1/payments/refunds", map[string]any{"orderId": id, "amount": bad})
			assert.Equal(t, 400, r.Status, "amount %d", bad)
			assert.Equal(t, CodeValidation, r.errCode())
		}

		rf := e.must(e.payments("POST", "/us/v1/payments/refunds", map[string]any{"orderId": id, "amount": 500}), 201)
		assert.Equal(t, p.str("paymentId"), rf.str("paymentId"))
		assert.EqualValues(t, 500, rf.num("amount"))
		assert.Equal(t, "partially_refunded", e.getOrder("us", id).str("paymentStatus"))

		r = e.payments("POST", "/us/v1/payments/refunds", map[string]any{"orderId": id, "amount": o.num("total")})
		assert.Equal(t, 422, r.Status, "more than the remaining balance")
		assert.Equal(t, CodeAmountMismatch, r.errCode())

		// Omitting the amount refunds the remaining balance.
		rf = e.must(e.payments("POST", "/us/v1/payments/refunds", map[string]any{"orderId": id}), 201)
		assert.EqualValues(t, o.num("total")-500, rf.num("amount"))
		assert.Equal(t, "refunded", e.getOrder("us", id).str("paymentStatus"))

		r = e.payments("POST", "/us/v1/payments/refunds", map[string]any{"orderId": id})
		assert.Equal(t, 409, r.Status)
		assert.Equal(t, CodePaymentNotRefundable, r.errCode())

		r = e.payments("POST", "/us/v1/payments/refunds", map[string]any{})
		assert.Equal(t, 400, r.Status)
		r = e.payments("POST", "/us/v1/payments/refunds", map[string]any{"orderId": "ord_9999"})
		assert.Equal(t, 404, r.Status)
	})
}

func TestDeleteOrder(t *testing.T) {
	e := newEnv(t, Options{})
	for _, state := range []string{orderCreated, orderShipped, orderReturned, orderCancelled} {
		o := e.orderIn("us", state)
		id := o.str("orderId")
		assert.Equal(t, 204, e.shop("DELETE", "/us/v1/orders/"+id, nil).Status, state)
		assert.Equal(t, 404, e.shop("GET", "/us/v1/orders/"+id, nil).Status, state)
	}
	assert.Equal(t, 404, e.shop("DELETE", "/us/v1/orders/ord_9999", nil).Status)
}

func TestReset(t *testing.T) {
	e := newEnv(t, Options{})
	id := e.newCart("us")
	e.must(e.shop("GET", "/us/v1/inventory/SKU-1004", nil), 200) // consume the stale read

	r := e.do("POST", e.api.URL+"/admin/reset", nil, nil)
	require.Equal(t, 200, r.Status)

	assert.Equal(t, 404, e.shop("GET", "/us/v1/carts/"+id, nil).Status, "data is wiped")
	assert.Equal(t, "cart_0001", e.newCart("us"), "sequences restart")
	assert.Equal(t, "ERROR", e.must(e.shop("GET", "/us/v1/inventory/SKU-1004", nil), 200).str("status"), "chaos counters restart")

	r = e.do("POST", e.api.URL+"/admin/reset", nil, nil)
	assert.Equal(t, 200, r.Status, "reset is idempotent")
}

func TestLatencyScaling(t *testing.T) {
	e := newEnv(t, Options{Latency: 0.05}) // 600 ms -> 30 ms, 350 ms -> 17.5 ms
	o := e.createOrder("us")
	start := time.Now()
	e.must(e.charge("us", o, methodCard, nil), 201)
	assert.GreaterOrEqual(t, time.Since(start), 15*time.Millisecond)
	start = time.Now()
	e.must(e.shop("POST", "/us/v1/orders/"+o.str("orderId")+"/ship", nil), 201)
	assert.GreaterOrEqual(t, time.Since(start), 25*time.Millisecond)
}

func TestBanner(t *testing.T) {
	s := New(Options{Latency: 1, Seed: 1})
	var buf bytes.Buffer
	s.Banner(&buf, "localhost:8765", "localhost:8766")
	out := buf.String()
	for _, want := range []string{
		"http://localhost:8765/{us,eu}/v1",
		"http://localhost:8766/{us,eu}/v1",
		"X-API-Key: pay-demo-key",
		"username=demo password=demo client_id=aat-shop client_secret=aat-shop-secret",
		"STALE_READ once per token",
		"POST http://localhost:8765/admin/reset",
	} {
		assert.Contains(t, out, want)
	}
	buf.Reset()
	New(Options{NoAuth: true}).Banner(&buf, "a", "b")
	assert.Contains(t, buf.String(), "auth:     disabled")
}

func TestMoneyFormatting(t *testing.T) {
	us, eu := regions["us"], regions["eu"]
	assert.Equal(t, "$0.05", us.format(5))
	assert.Equal(t, "-$1.00", us.format(-100))
	assert.Equal(t, "€123.45", eu.format(12345))
	assert.EqualValues(t, 8279, eu.price(8999))
	assert.EqualValues(t, 742, us.tax(8999))
	assert.EqualValues(t, 1380, eu.tax(8279))
	assert.Equal(t, fmt.Sprintf("%d", 900), fmt.Sprintf("%d", percentOff(8999, 10)))
}
