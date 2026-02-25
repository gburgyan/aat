package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutorRouter_DefaultRoute(t *testing.T) {
	defaultExec := adapter.NewHTTPExecutor("https://default.example.com")
	defaultCfg := &adapter.EnvironmentConfig{BaseURL: "https://default.example.com"}

	router := NewExecutorRouter(defaultExec, defaultCfg)

	exec, cfg, rewrite := router.Resolve("anyNode")
	assert.Equal(t, defaultExec, exec)
	assert.Equal(t, defaultCfg, cfg)
	assert.Nil(t, rewrite)
}

func TestExecutorRouter_ExactMatch(t *testing.T) {
	defaultExec := adapter.NewHTTPExecutor("https://default.example.com")
	overrideExec := adapter.NewHTTPExecutor("http://localhost:8080")
	overrideCfg := &adapter.EnvironmentConfig{BaseURL: "http://localhost:8080"}

	router := NewExecutorRouter(defaultExec, &adapter.EnvironmentConfig{})
	router.AddOverride("searchFlights", overrideExec, overrideCfg, nil)

	exec, cfg, rewrite := router.Resolve("searchFlights")
	assert.Equal(t, overrideExec, exec)
	assert.Equal(t, overrideCfg, cfg)
	assert.Nil(t, rewrite)

	// Non-matching node gets default
	exec, _, _ = router.Resolve("bookFlight")
	assert.Equal(t, defaultExec, exec)
}

func TestExecutorRouter_GlobMatch(t *testing.T) {
	defaultExec := adapter.NewHTTPExecutor("https://default.example.com")
	priceExec := adapter.NewHTTPExecutor("https://price.example.com")
	priceCfg := &adapter.EnvironmentConfig{BaseURL: "https://price.example.com"}

	router := NewExecutorRouter(defaultExec, &adapter.EnvironmentConfig{})
	router.AddOverride("price*", priceExec, priceCfg, nil)

	exec, cfg, _ := router.Resolve("priceOffer")
	assert.Equal(t, priceExec, exec)
	assert.Equal(t, priceCfg, cfg)

	exec, cfg, _ = router.Resolve("priceTicket")
	assert.Equal(t, priceExec, exec)
	assert.Equal(t, priceCfg, cfg)

	// Non-matching
	exec, _, _ = router.Resolve("searchFlights")
	assert.Equal(t, defaultExec, exec)
}

func TestExecutorRouter_ExactBeforeGlob(t *testing.T) {
	defaultExec := adapter.NewHTTPExecutor("https://default.example.com")
	globExec := adapter.NewHTTPExecutor("https://glob.example.com")
	exactExec := adapter.NewHTTPExecutor("https://exact.example.com")

	router := NewExecutorRouter(defaultExec, &adapter.EnvironmentConfig{})
	router.AddOverride("price*", globExec, &adapter.EnvironmentConfig{}, nil)
	router.AddOverride("priceOffer", exactExec, &adapter.EnvironmentConfig{}, nil)

	// Exact match wins over glob
	exec, _, _ := router.Resolve("priceOffer")
	assert.Equal(t, exactExec, exec)

	// Glob still works for non-exact
	exec, _, _ = router.Resolve("priceTicket")
	assert.Equal(t, globExec, exec)
}

func TestExecutorRouter_WithPathRewrite(t *testing.T) {
	defaultExec := adapter.NewHTTPExecutor("https://default.example.com")
	overrideExec := adapter.NewHTTPExecutor("http://localhost:8080")
	rewrite := &adapter.PathRewrite{Strip: "/11", Prefix: "/api/v2"}

	router := NewExecutorRouter(defaultExec, &adapter.EnvironmentConfig{})
	router.AddOverride("searchFlights", overrideExec, &adapter.EnvironmentConfig{}, rewrite)

	_, _, rw := router.Resolve("searchFlights")
	assert.NotNil(t, rw)
	assert.Equal(t, "/11", rw.Strip)
	assert.Equal(t, "/api/v2", rw.Prefix)
}

func TestExecutorRouter_FirstGlobWins(t *testing.T) {
	defaultExec := adapter.NewHTTPExecutor("https://default.example.com")
	firstExec := adapter.NewHTTPExecutor("https://first.example.com")
	secondExec := adapter.NewHTTPExecutor("https://second.example.com")

	router := NewExecutorRouter(defaultExec, &adapter.EnvironmentConfig{})
	router.AddOverride("search*", firstExec, &adapter.EnvironmentConfig{}, nil)
	router.AddOverride("search*", secondExec, &adapter.EnvironmentConfig{}, nil)

	exec, _, _ := router.Resolve("searchFlights")
	assert.Equal(t, firstExec, exec)
}

func TestExecutorRouter_HasOverrides(t *testing.T) {
	router := NewExecutorRouter(adapter.NewHTTPExecutor("https://default.example.com"), &adapter.EnvironmentConfig{})
	assert.False(t, router.HasOverrides())

	router.AddOverride("node1", adapter.NewHTTPExecutor("http://localhost"), &adapter.EnvironmentConfig{}, nil)
	assert.True(t, router.HasOverrides())
}

func TestExecutorRouter_OverridePatterns(t *testing.T) {
	router := NewExecutorRouter(adapter.NewHTTPExecutor("https://default.example.com"), &adapter.EnvironmentConfig{})
	router.AddOverride("searchFlights", adapter.NewHTTPExecutor("http://localhost:8080"), &adapter.EnvironmentConfig{}, nil)
	router.AddOverride("price*", adapter.NewHTTPExecutor("http://localhost:8081"), &adapter.EnvironmentConfig{}, nil)

	patterns := router.OverridePatterns()
	assert.Equal(t, []string{"searchFlights", "price*"}, patterns)
}

func TestIsGlobPattern(t *testing.T) {
	assert.True(t, isGlobPattern("price*"))
	assert.True(t, isGlobPattern("node?"))
	assert.True(t, isGlobPattern("[abc]"))
	assert.False(t, isGlobPattern("exactMatch"))
	assert.False(t, isGlobPattern("searchFlights"))
}

func TestExecutorRouter_QuestionMarkGlob(t *testing.T) {
	defaultExec := adapter.NewHTTPExecutor("https://default.example.com")
	overrideExec := adapter.NewHTTPExecutor("http://localhost:8080")

	router := NewExecutorRouter(defaultExec, &adapter.EnvironmentConfig{})
	router.AddOverride("step?", overrideExec, &adapter.EnvironmentConfig{}, nil)

	exec, _, _ := router.Resolve("step1")
	assert.Equal(t, overrideExec, exec)

	exec, _, _ = router.Resolve("step2")
	assert.Equal(t, overrideExec, exec)

	exec, _, _ = router.Resolve("step12") // two chars after "step" — no match
	assert.Equal(t, defaultExec, exec)
}

// TestEngine_Run_RoutingToMultipleServers verifies that a 2-step plan routes
// each step to a different HTTP server based on override configuration.
func TestEngine_Run_RoutingToMultipleServers(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "test.search",
				Inputs:  []graph.Input{{Name: "query", Type: "string"}},
				Outputs: []graph.Output{{Name: "resultId", Type: "string"}},
			},
			"book": {
				Name:    "book",
				Adapter: "test.book",
				Inputs:  []graph.Input{{Name: "resultId", Type: "string"}},
				Outputs: []graph.Output{{Name: "bookingId", Type: "string"}},
			},
		},
	}

	// Server 1: search service (local override)
	var searchHost string
	searchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchHost = r.Host
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"resultId": "found-123"})
	}))
	defer searchServer.Close()

	// Server 2: booking service (default)
	var bookHost string
	bookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bookHost = r.Host
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"bookingId": "book-456"})
	}))
	defer bookServer.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.search", &stubAdapter{
		method:   "POST",
		path:     "/search",
		response: map[string]any{"resultId": "found-123"},
	}))
	require.NoError(t, registry.Register("test.book", &stubAdapter{
		method:   "POST",
		path:     "/book",
		response: map[string]any{"bookingId": "book-456"},
	}))

	// Default executor points to booking server
	defaultExec := adapter.NewHTTPExecutor(bookServer.URL)
	defaultCfg := &adapter.EnvironmentConfig{BaseURL: bookServer.URL}

	// Override: search goes to search server
	searchExec := adapter.NewHTTPExecutor(searchServer.URL)
	searchCfg := &adapter.EnvironmentConfig{BaseURL: searchServer.URL}

	router := NewExecutorRouter(defaultExec, defaultCfg)
	router.AddOverride("search", searchExec, searchCfg, nil)

	eng := NewEngine(g, registry, router)

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{"query": {Default: "test"}}},
				{Node: "book", DependsOn: []string{"search"}, Values: map[string]plan.StepValue{
					"resultId": {From: "search.resultId"},
				}},
			},
		},
	}

	result := eng.Run(context.Background(), p)

	assert.Equal(t, OutcomePassed, result.Outcome)
	assert.Nil(t, result.Error)
	require.Len(t, result.Steps, 2)

	// Verify the two steps hit different servers
	assert.NotEmpty(t, searchHost)
	assert.NotEmpty(t, bookHost)
	assert.NotEqual(t, searchHost, bookHost, "search and book should hit different servers")
}

// TestEngine_Run_PathRewriteIntegration verifies that path rewriting works end-to-end.
func TestEngine_Run_PathRewriteIntegration(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"search": {
				Name:    "search",
				Adapter: "test.search",
				Inputs:  []graph.Input{{Name: "query", Type: "string"}},
				Outputs: []graph.Output{{Name: "id", Type: "string"}},
			},
		},
	}

	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	registry := adapter.NewRegistry()
	require.NoError(t, registry.Register("test.search", &stubAdapter{
		method:   "POST",
		path:     "/11/search/flights", // template path includes /11 prefix
		response: map[string]any{"id": "x"},
	}))

	exec := adapter.NewHTTPExecutor(server.URL)
	router := NewExecutorRouter(exec, &adapter.EnvironmentConfig{})
	router.AddOverride("search", exec, &adapter.EnvironmentConfig{},
		&adapter.PathRewrite{Strip: "/11", Prefix: "/api/v2"})

	eng := NewEngine(g, registry, router)

	p := &plan.Plan{
		Metadata: plan.Metadata{GraphVersion: "1.0.0"},
		Execution: plan.Execution{
			Steps: []plan.Step{
				{Node: "search", Values: map[string]plan.StepValue{"query": {Default: "test"}}},
			},
		},
	}

	result := eng.Run(context.Background(), p)
	assert.Equal(t, OutcomePassed, result.Outcome)

	// Path should be rewritten: /11/search/flights → /api/v2/search/flights
	assert.Equal(t, "/api/v2/search/flights", receivedPath)
}
