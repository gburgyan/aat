package plan

import (
	"strings"
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mutationTestGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"createBooking": {
				Name:    "createBooking",
				Adapter: "test.createBooking",
				Inputs: []graph.Input{
					{Name: "lastName", Type: "string"},
					{Name: "age", Type: "integer"},
				},
				Outputs: []graph.Output{{Name: "bookingId", Type: "string"}},
			},
		},
	}
}

func TestExpandMutations_EmitsSiblingPerMutation(t *testing.T) {
	g := mutationTestGraph()
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					ID:   "happy",
					Node: "createBooking",
					Values: map[string]StepValue{
						"lastName": {Default: "Smith"},
						"age":      {Default: 30},
					},
					Mutations: []Mutation{
						{Name: "empty-lastName", Set: map[string]any{"lastName": ""}, ExpectStatus: []int{400}},
						{Name: "negative-age", Set: map[string]any{"age": -1}, ExpectStatus: []int{400, 422}, Description: "age must be positive"},
						{Name: "malformed-body", RawBody: `{"oops":`, ExpectStatus: []int{400}},
					},
				},
			},
		},
	}

	inst, err := InstantiateAndValidate(p, g)
	require.NoError(t, err)
	require.Len(t, inst.Execution.Steps, 4, "parent + 3 siblings")

	parent := inst.Execution.Steps[0]
	assert.Equal(t, "happy", parent.ID)
	assert.Empty(t, parent.Mutations, "parent should no longer carry its mutations post-expansion")
	assert.Nil(t, parent.ExpectFailure, "parent retains happy-path semantics")

	empty := inst.Execution.Steps[1]
	assert.Equal(t, "happy--empty-lastName", empty.ID)
	assert.Equal(t, "createBooking", empty.Node)
	assert.Equal(t, "", empty.Values["lastName"].Default, "Set overwrites parent's value")
	assert.Equal(t, 30, empty.Values["age"].Default, "non-overridden values inherited from parent")
	require.NotNil(t, empty.ExpectFailure)
	assert.Equal(t, []int{400}, empty.ExpectFailure.Status)
	assert.Empty(t, empty.RawBody)
	assert.Empty(t, empty.Mutations)

	neg := inst.Execution.Steps[2]
	assert.Equal(t, "happy--negative-age", neg.ID)
	assert.Equal(t, -1, neg.Values["age"].Default)
	require.NotNil(t, neg.ExpectFailure)
	assert.Equal(t, []int{400, 422}, neg.ExpectFailure.Status)
	assert.Equal(t, "age must be positive", neg.ExpectFailure.Description)

	raw := inst.Execution.Steps[3]
	assert.Equal(t, "happy--malformed-body", raw.ID)
	assert.Equal(t, `{"oops":`, raw.RawBody)
	require.NotNil(t, raw.ExpectFailure)
}

func TestExpandMutations_NoParentID_FallsBackToNodeName(t *testing.T) {
	g := mutationTestGraph()
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					Node: "createBooking",
					Values: map[string]StepValue{
						"lastName": {Default: "Smith"},
						"age":      {Default: 30},
					},
					Mutations: []Mutation{
						{Name: "bad", Set: map[string]any{"age": -1}, ExpectStatus: []int{400}},
					},
				},
			},
		},
	}
	inst, err := InstantiateAndValidate(p, g)
	require.NoError(t, err)
	require.Len(t, inst.Execution.Steps, 2)
	assert.Equal(t, "createBooking--bad", inst.Execution.Steps[1].ID, "sibling ID falls back to parent node name when parent has no ID")
}

func TestExpandMutations_InheritsDependsOn(t *testing.T) {
	g := &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"setup":         {Name: "setup", Adapter: "t.setup", Outputs: []graph.Output{{Name: "token", Type: "string"}}},
			"createBooking": mutationTestGraph().Nodes["createBooking"],
		},
	}
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{Node: "setup"},
				{
					Node:      "createBooking",
					DependsOn: []string{"setup"},
					Values: map[string]StepValue{
						"lastName": {Default: "Smith"},
						"age":      {Default: 30},
					},
					Mutations: []Mutation{
						{Name: "bad", Set: map[string]any{"age": -1}, ExpectStatus: []int{400}},
					},
				},
			},
		},
	}
	inst, err := InstantiateAndValidate(p, g)
	require.NoError(t, err)
	require.Len(t, inst.Execution.Steps, 3)
	sibling := inst.Execution.Steps[2]
	assert.Equal(t, []string{"setup"}, sibling.DependsOn, "mutations share the parent's prereq chain")
}

// isolatedGraph is a three-node graph used for closure-cloning tests:
// login -> createCart -> addItem (+ cleanup pairing on createCart).
func isolatedGraph() *graph.Graph {
	return &graph.Graph{
		Version: "1.0.0",
		Nodes: map[string]*graph.Node{
			"login": {
				Name:    "login",
				Adapter: "t.login",
				Outputs: []graph.Output{{Name: "token", Type: "string"}},
			},
			"createCart": {
				Name:    "createCart",
				Adapter: "t.createCart",
				Inputs: []graph.Input{
					{Name: "token", Type: "string"},
				},
				Outputs: []graph.Output{{Name: "cartId", Type: "string"}},
				Cleanup: "deleteCart",
			},
			"deleteCart": {
				Name:    "deleteCart",
				Adapter: "t.deleteCart",
				Inputs:  []graph.Input{{Name: "cartId", Type: "string"}},
			},
			"addItem": {
				Name:    "addItem",
				Adapter: "t.addItem",
				Inputs: []graph.Input{
					{Name: "cartId", Type: "string"},
					{Name: "productId", Type: "string"},
				},
			},
		},
	}
}

// isolatedBasePlan returns a login -> createCart -> addItem plan wired via
// `from` refs. Callers append mutations and set MutationScope as needed.
func isolatedBasePlan() *Plan {
	return &Plan{
		Execution: Execution{
			Steps: []Step{
				{ID: "login", Node: "login"},
				{
					ID:        "createCart",
					Node:      "createCart",
					DependsOn: []string{"login"},
					Values: map[string]StepValue{
						"token": {From: "login.token"},
					},
				},
				{
					ID:        "addItem",
					Node:      "addItem",
					DependsOn: []string{"createCart"},
					Values: map[string]StepValue{
						"cartId":    {From: "createCart.cartId"},
						"productId": {Default: "P1"},
					},
				},
			},
		},
	}
}

func TestExpandMutations_Isolated_ClonesClosure(t *testing.T) {
	g := isolatedGraph()
	p := isolatedBasePlan()
	addItem := &p.Execution.Steps[2]
	addItem.MutationScope = "isolated"
	addItem.Mutations = []Mutation{
		{Name: "empty-productId", Set: map[string]any{"productId": ""}, ExpectStatus: []int{400}},
		{Name: "unknown-productId", Set: map[string]any{"productId": "NO-SUCH"}, ExpectStatus: []int{404}},
	}

	inst, err := InstantiateAndValidate(p, g)
	require.NoError(t, err)

	// Expected emission: login, createCart, addItem-happy,
	//   login__empty-productId, createCart__empty-productId, addItem--empty-productId,
	//   login__unknown-productId, createCart__unknown-productId, addItem--unknown-productId
	require.Len(t, inst.Execution.Steps, 9)

	ids := make([]string, len(inst.Execution.Steps))
	for i, s := range inst.Execution.Steps {
		ids[i] = s.StepID()
	}
	assert.Equal(t, []string{
		"login", "createCart", "addItem",
		"login__empty-productId", "createCart__empty-productId", "addItem--empty-productId",
		"login__unknown-productId", "createCart__unknown-productId", "addItem--unknown-productId",
	}, ids)

	// Cloned createCart's dependsOn points at the cloned login, not the original.
	cart1 := stepByID(t, inst, "createCart__empty-productId")
	assert.Equal(t, []string{"login__empty-productId"}, cart1.DependsOn)

	// Mutation sibling's dependsOn points at the cloned cart.
	m1 := stepByID(t, inst, "addItem--empty-productId")
	assert.Equal(t, []string{"createCart__empty-productId"}, m1.DependsOn)

	// And mutation 2 uses its own clone chain — no crosstalk with mutation 1.
	cart2 := stepByID(t, inst, "createCart__unknown-productId")
	assert.Equal(t, []string{"login__unknown-productId"}, cart2.DependsOn)
	m2 := stepByID(t, inst, "addItem--unknown-productId")
	assert.Equal(t, []string{"createCart__unknown-productId"}, m2.DependsOn)
}

func TestExpandMutations_Isolated_RewritesValueRefs(t *testing.T) {
	g := isolatedGraph()
	p := isolatedBasePlan()
	addItem := &p.Execution.Steps[2]
	addItem.MutationScope = "isolated"
	addItem.Mutations = []Mutation{
		{Name: "bad", Set: map[string]any{"productId": ""}, ExpectStatus: []int{400}},
	}

	inst, err := InstantiateAndValidate(p, g)
	require.NoError(t, err)

	// Cloned createCart pulls its token from the cloned login.
	cart := stepByID(t, inst, "createCart__bad")
	assert.Equal(t, "login__bad.token", cart.Values["token"].From)

	// Mutation sibling pulls its cartId from the cloned createCart, not the original.
	sib := stepByID(t, inst, "addItem--bad")
	assert.Equal(t, "createCart__bad.cartId", sib.Values["cartId"].From)

	// Happy-path parent is untouched.
	parent := stepByID(t, inst, "addItem")
	assert.Equal(t, "createCart.cartId", parent.Values["cartId"].From)
}

func TestExpandMutations_Isolated_DiamondDependency(t *testing.T) {
	// login -> createCart, login -> createUser, addItem depends on both.
	g := isolatedGraph()
	g.Nodes["createUser"] = &graph.Node{
		Name:    "createUser",
		Adapter: "t.createUser",
		Inputs:  []graph.Input{{Name: "token", Type: "string"}},
		Outputs: []graph.Output{{Name: "userId", Type: "string"}},
	}
	g.Nodes["addItem"].Inputs = append(g.Nodes["addItem"].Inputs, graph.Input{Name: "userId", Type: "string"})

	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{ID: "login", Node: "login"},
				{
					ID:        "createCart",
					Node:      "createCart",
					DependsOn: []string{"login"},
					Values:    map[string]StepValue{"token": {From: "login.token"}},
				},
				{
					ID:        "createUser",
					Node:      "createUser",
					DependsOn: []string{"login"},
					Values:    map[string]StepValue{"token": {From: "login.token"}},
				},
				{
					ID:            "addItem",
					Node:          "addItem",
					DependsOn:     []string{"createCart", "createUser"},
					MutationScope: "isolated",
					Values: map[string]StepValue{
						"cartId":    {From: "createCart.cartId"},
						"userId":    {From: "createUser.userId"},
						"productId": {Default: "P1"},
					},
					Mutations: []Mutation{
						{Name: "bad", Set: map[string]any{"productId": ""}, ExpectStatus: []int{400}},
					},
				},
			},
		},
	}
	inst, err := InstantiateAndValidate(p, g)
	require.NoError(t, err)

	// One clone per closure step, and the shared ancestor (login) is cloned
	// exactly once within the mutation's subgraph.
	require.Len(t, inst.Execution.Steps, 4+3+1)

	loginClones := 0
	for _, s := range inst.Execution.Steps {
		if s.StepID() == "login__bad" {
			loginClones++
		}
	}
	assert.Equal(t, 1, loginClones, "shared ancestor cloned exactly once per mutation")

	sib := stepByID(t, inst, "addItem--bad")
	assert.ElementsMatch(t, []string{"createCart__bad", "createUser__bad"}, sib.DependsOn)
	assert.Equal(t, "createCart__bad.cartId", sib.Values["cartId"].From)
	assert.Equal(t, "createUser__bad.userId", sib.Values["userId"].From)
}

func TestExpandMutations_Isolated_EmptyClosure(t *testing.T) {
	g := mutationTestGraph()
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{
					ID:            "createBooking",
					Node:          "createBooking",
					MutationScope: "isolated",
					Values: map[string]StepValue{
						"lastName": {Default: "Smith"},
						"age":      {Default: 30},
					},
					Mutations: []Mutation{
						{Name: "bad", Set: map[string]any{"age": -1}, ExpectStatus: []int{400}},
					},
				},
			},
		},
	}
	inst, err := InstantiateAndValidate(p, g)
	require.NoError(t, err)
	require.Len(t, inst.Execution.Steps, 2, "isolated with no prereqs degrades to shared — parent plus single sibling")
	assert.Equal(t, "createBooking--bad", inst.Execution.Steps[1].ID)
}

func TestExpandMutations_Shared_DefaultUnchanged(t *testing.T) {
	// Guard against regression: a step without MutationScope runs under the
	// original shared semantics — no prereq cloning.
	g := isolatedGraph()
	p := isolatedBasePlan()
	addItem := &p.Execution.Steps[2]
	addItem.Mutations = []Mutation{
		{Name: "bad", Set: map[string]any{"productId": ""}, ExpectStatus: []int{400}},
	}
	inst, err := InstantiateAndValidate(p, g)
	require.NoError(t, err)
	require.Len(t, inst.Execution.Steps, 4, "login + createCart + parent + single sibling")
	sib := stepByID(t, inst, "addItem--bad")
	assert.Equal(t, []string{"createCart"}, sib.DependsOn, "shared mode points at the original chain")
	assert.Equal(t, "createCart.cartId", sib.Values["cartId"].From)
}

func TestValidateMutationsSyntax_UnknownScope(t *testing.T) {
	p := &Plan{
		Execution: Execution{
			Steps: []Step{{
				ID:            "x",
				Node:          "createBooking",
				MutationScope: "bogus",
				Mutations: []Mutation{
					{Name: "m", Set: map[string]any{"a": 1}, ExpectStatus: []int{400}},
				},
			}},
		},
	}
	errs := validateMutationsSyntax(p)
	require.NotEmpty(t, errs)
	joined := strings.Join(errs, "\n")
	assert.Contains(t, joined, "unknown mutationScope")
}

func TestValidateMutationsSyntax_ScopeWithoutMutations(t *testing.T) {
	p := &Plan{
		Execution: Execution{
			Steps: []Step{{
				ID:            "x",
				Node:          "createBooking",
				MutationScope: "isolated",
			}},
		},
	}
	errs := validateMutationsSyntax(p)
	require.NotEmpty(t, errs)
	assert.Contains(t, strings.Join(errs, "\n"), "mutationScope is set but step has no mutations")
}

func TestInstantiateAndValidate_IsolatedCollision(t *testing.T) {
	g := isolatedGraph()
	p := isolatedBasePlan()
	// User already has a step whose id collides with a would-be clone.
	p.Execution.Steps = append(p.Execution.Steps, Step{
		ID:   "login__bad",
		Node: "login",
	})
	addItem := &p.Execution.Steps[2]
	addItem.MutationScope = "isolated"
	addItem.Mutations = []Mutation{
		{Name: "bad", Set: map[string]any{"productId": ""}, ExpectStatus: []int{400}},
	}
	_, err := InstantiateAndValidate(p, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides with an existing step")
}

// stepByID finds a step by its effective StepID. Fails the test if missing.
func stepByID(t *testing.T, p *Plan, id string) Step {
	t.Helper()
	for _, s := range p.Execution.Steps {
		if s.StepID() == id {
			return s
		}
	}
	t.Fatalf("step %q not found in instantiated plan", id)
	return Step{}
}

func TestStripMutations_ClearsFieldsLeavesRest(t *testing.T) {
	p := &Plan{
		Intent: Intent{Goal: "addItem"},
		Execution: Execution{
			Steps: []Step{
				{ID: "login", Node: "login"},
				{
					ID:            "addItem",
					Node:          "addItem",
					DependsOn:     []string{"login"},
					MutationScope: "isolated",
					Values: map[string]StepValue{
						"productId": {Default: "P1"},
					},
					ExpectFailure: &ExpectFailure{Status: []int{404}},
					RawBody:       `{"raw":"body"}`,
					Mutations: []Mutation{
						{Name: "bad", Set: map[string]any{"productId": ""}, ExpectStatus: []int{400}},
					},
				},
			},
		},
	}

	StripMutations(p)

	require.Len(t, p.Execution.Steps, 2)
	parent := p.Execution.Steps[1]
	assert.Empty(t, parent.Mutations, "Mutations cleared")
	assert.Empty(t, parent.MutationScope, "MutationScope cleared")

	// Unrelated fields preserved.
	assert.Equal(t, "addItem", parent.ID)
	assert.Equal(t, []string{"login"}, parent.DependsOn)
	assert.Equal(t, "P1", parent.Values["productId"].Default)
	assert.Equal(t, `{"raw":"body"}`, parent.RawBody, "standalone rawBody preserved")
	require.NotNil(t, parent.ExpectFailure)
	assert.Equal(t, []int{404}, parent.ExpectFailure.Status, "standalone expectFailure preserved")
	assert.Equal(t, "addItem", p.Intent.Goal, "plan intent preserved")
}

func TestStripMutations_NilSafe(t *testing.T) {
	StripMutations(nil) // must not panic
}

func TestStripMutations_ThenInstantiateOnlyHappyPath(t *testing.T) {
	g := isolatedGraph()
	p := isolatedBasePlan()
	addItem := &p.Execution.Steps[2]
	addItem.MutationScope = "isolated"
	addItem.Mutations = []Mutation{
		{Name: "bad", Set: map[string]any{"productId": ""}, ExpectStatus: []int{400}},
		{Name: "worse", Set: map[string]any{"productId": "NO"}, ExpectStatus: []int{404}},
	}

	StripMutations(p)
	inst, err := InstantiateAndValidate(p, g)
	require.NoError(t, err)
	require.Len(t, inst.Execution.Steps, 3, "login + createCart + addItem happy-path only — no clones, no siblings")
	assert.Equal(t, "addItem", inst.Execution.Steps[2].ID)
	assert.Empty(t, inst.Execution.Steps[2].Mutations)
}

func TestValidateMutationsSyntax(t *testing.T) {
	cases := []struct {
		name        string
		mutations   []Mutation
		wantErrLike string
	}{
		{
			name:        "empty name",
			mutations:   []Mutation{{Name: "", Set: map[string]any{"age": 1}, ExpectStatus: []int{400}}},
			wantErrLike: "empty name",
		},
		{
			name: "duplicate names",
			mutations: []Mutation{
				{Name: "dup", Set: map[string]any{"age": 1}, ExpectStatus: []int{400}},
				{Name: "dup", Set: map[string]any{"age": 2}, ExpectStatus: []int{400}},
			},
			wantErrLike: "duplicate mutation name",
		},
		{
			name:        "missing set and rawBody",
			mutations:   []Mutation{{Name: "nothing", ExpectStatus: []int{400}}},
			wantErrLike: "at least one of set or rawBody",
		},
		{
			name:        "missing expectStatus",
			mutations:   []Mutation{{Name: "bad", Set: map[string]any{"a": 1}}},
			wantErrLike: "at least one expectStatus",
		},
		{
			name:        "expectStatus below 400",
			mutations:   []Mutation{{Name: "bad", Set: map[string]any{"a": 1}, ExpectStatus: []int{200}}},
			wantErrLike: ">= 400",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Plan{
				Execution: Execution{
					Steps: []Step{{Node: "createBooking", ID: "x", Mutations: tc.mutations}},
				},
			}
			errs := validateMutationsSyntax(p)
			require.NotEmpty(t, errs, "expected a validation error for %s", tc.name)
			found := false
			for _, e := range errs {
				if strings.Contains(e, tc.wantErrLike) {
					found = true
					break
				}
			}
			assert.True(t, found, "expected error containing %q, got %v", tc.wantErrLike, errs)
		})
	}
}
