package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseFieldType(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    FieldType
		wantErr bool
	}{
		// Scalar types
		{name: "string", raw: "string", want: FieldType{Kind: TypeScalar, Name: "string"}},
		{name: "integer", raw: "integer", want: FieldType{Kind: TypeScalar, Name: "integer"}},
		{name: "float", raw: "float", want: FieldType{Kind: TypeScalar, Name: "float"}},
		{name: "boolean", raw: "boolean", want: FieldType{Kind: TypeScalar, Name: "boolean"}},
		{name: "date", raw: "date", want: FieldType{Kind: TypeScalar, Name: "date"}},
		{name: "datetime", raw: "datetime", want: FieldType{Kind: TypeScalar, Name: "datetime"}},
		{name: "money", raw: "money", want: FieldType{Kind: TypeScalar, Name: "money"}},

		// Enum types
		{
			name: "enum with multiple values",
			raw:  "enum[economy, business, first]",
			want: FieldType{Kind: TypeEnum, Name: "enum", EnumValues: []string{"economy", "business", "first"}},
		},
		{
			name: "enum with single value",
			raw:  "enum[active]",
			want: FieldType{Kind: TypeEnum, Name: "enum", EnumValues: []string{"active"}},
		},
		{
			name: "enum with extra whitespace",
			raw:  "enum[ a , b , c ]",
			want: FieldType{Kind: TypeEnum, Name: "enum", EnumValues: []string{"a", "b", "c"}},
		},

		// Array types
		{
			name: "flight array",
			raw:  "flight[]",
			want: FieldType{Kind: TypeArray, Name: "flight", IsArray: true},
		},
		{
			name: "string array",
			raw:  "string[]",
			want: FieldType{Kind: TypeArray, Name: "string", IsArray: true},
		},

		// Custom domain types
		{
			name: "airportCode",
			raw:  "airportCode",
			want: FieldType{Kind: TypeCustom, Name: "airportCode"},
		},
		{
			name: "customDomain",
			raw:  "customDomain",
			want: FieldType{Kind: TypeCustom, Name: "customDomain"},
		},

		// Error cases
		{name: "empty string", raw: "", wantErr: true},
		{name: "whitespace only", raw: "   ", wantErr: true},
		{name: "enum no values", raw: "enum[]", wantErr: true},
		{name: "enum malformed", raw: "enum[a, b", wantErr: true},
		{name: "empty array element", raw: "[]", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFieldType(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.Kind, got.Kind, "Kind")
			assert.Equal(t, tt.want.Name, got.Name, "Name")
			assert.Equal(t, tt.want.EnumValues, got.EnumValues, "EnumValues")
			assert.Equal(t, tt.want.IsArray, got.IsArray, "IsArray")
		})
	}
}

func TestInputDefault_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		wantValue     any
		wantPool      []any
		wantFrom      string
		wantResolved  string
		wantConstr    string
		wantSelect    *InputDefaultSelect
		wantLiteral   bool
	}{
		{
			name:        "scalar string",
			yaml:        "name: origin\ntype: string\ndefault: ADT\n",
			wantValue:   "ADT",
			wantLiteral: true,
		},
		{
			name:        "scalar int",
			yaml:        "name: count\ntype: integer\ndefault: 1\n",
			wantValue:   1,
			wantLiteral: true,
		},
		{
			name:        "scalar bool",
			yaml:        "name: active\ntype: boolean\ndefault: true\n",
			wantValue:   true,
			wantLiteral: true,
		},
		{
			name:     "pool shorthand",
			yaml:     "name: origin\ntype: airportCode\ndefault: [DEN, ORD, ATL]\n",
			wantPool: []any{"DEN", "ORD", "ATL"},
		},
		{
			name:       "pool with constraint",
			yaml:       "name: origin\ntype: airportCode\ndefault:\n  pool: [DEN, ORD]\n  constraint: \"value != destination\"\n",
			wantPool:   []any{"DEN", "ORD"},
			wantConstr: "value != destination",
		},
		{
			name:     "from reference",
			yaml:     "name: workbenchId\ntype: string\ndefault:\n  from: createWorkbench.workbenchId\n",
			wantFrom: "createWorkbench.workbenchId",
		},
		{
			name:     "from with select",
			yaml:     "name: offeringId\ntype: string\ndefault:\n  from: searchFlights.offerings\n  select:\n    strategy: first\n    field: offeringId\n",
			wantFrom: "searchFlights.offerings",
			wantSelect: &InputDefaultSelect{
				Strategy: "first",
				Field:    "offeringId",
			},
		},
		{
			name:         "fromResolved",
			yaml:         "name: destination\ntype: airportCode\ndefault:\n  fromResolved: leg1Destination\n",
			wantResolved: "leg1Destination",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var inp Input
			err := yaml.Unmarshal([]byte(tt.yaml), &inp)
			require.NoError(t, err, "unmarshal should succeed")
			require.NotNil(t, inp.Default, "Default should not be nil")

			d := inp.Default

			if tt.wantValue != nil {
				assert.Equal(t, tt.wantValue, d.Value, "Value")
			}
			if tt.wantPool != nil {
				assert.Equal(t, tt.wantPool, d.Pool, "Pool")
			}
			if tt.wantFrom != "" {
				assert.Equal(t, tt.wantFrom, d.From, "From")
			}
			if tt.wantResolved != "" {
				assert.Equal(t, tt.wantResolved, d.FromResolved, "FromResolved")
			}
			if tt.wantConstr != "" {
				assert.Equal(t, tt.wantConstr, d.Constraint, "Constraint")
			}
			if tt.wantSelect != nil {
				require.NotNil(t, d.Select, "Select should not be nil")
				assert.Equal(t, tt.wantSelect.Strategy, d.Select.Strategy, "Select.Strategy")
				assert.Equal(t, tt.wantSelect.Field, d.Select.Field, "Select.Field")
			}
			if tt.wantLiteral {
				assert.True(t, d.IsLiteralOnly(), "IsLiteralOnly")
			} else {
				assert.False(t, d.IsLiteralOnly(), "IsLiteralOnly should be false for non-literal defaults")
			}
		})
	}
}

func TestInputDefault_MarshalYAML(t *testing.T) {
	t.Run("literal only emits bare scalar", func(t *testing.T) {
		inp := Input{
			Name:    "origin",
			Type:    "string",
			Default: LiteralDefault("ADT"),
		}
		data, err := yaml.Marshal(&inp)
		require.NoError(t, err)

		// Round-trip: unmarshal back and verify the value survived
		var got Input
		err = yaml.Unmarshal(data, &got)
		require.NoError(t, err)
		require.NotNil(t, got.Default)
		assert.Equal(t, "ADT", got.Default.Value)
		assert.True(t, got.Default.IsLiteralOnly())
	})

	t.Run("pool emits map with pool key", func(t *testing.T) {
		inp := Input{
			Name: "origin",
			Type: "airportCode",
			Default: &InputDefault{
				Pool: []any{"DEN", "ORD"},
			},
		}
		data, err := yaml.Marshal(&inp)
		require.NoError(t, err)

		var got Input
		err = yaml.Unmarshal(data, &got)
		require.NoError(t, err)
		require.NotNil(t, got.Default)
		assert.Equal(t, []any{"DEN", "ORD"}, got.Default.Pool)
		assert.Nil(t, got.Default.Value)
	})

	t.Run("from emits map with from key", func(t *testing.T) {
		inp := Input{
			Name: "workbenchId",
			Type: "string",
			Default: &InputDefault{
				From: "createWorkbench.workbenchId",
			},
		}
		data, err := yaml.Marshal(&inp)
		require.NoError(t, err)

		var got Input
		err = yaml.Unmarshal(data, &got)
		require.NoError(t, err)
		require.NotNil(t, got.Default)
		assert.Equal(t, "createWorkbench.workbenchId", got.Default.From)
	})
}

func TestInputDefault_HelperMethods(t *testing.T) {
	t.Run("nil InputDefault", func(t *testing.T) {
		var d *InputDefault
		assert.False(t, d.IsLiteralOnly())
		assert.False(t, d.HasValue())
		assert.Nil(t, d.EffectiveValue())
	})

	t.Run("literal only", func(t *testing.T) {
		d := LiteralDefault("hello")
		assert.True(t, d.IsLiteralOnly())
		assert.True(t, d.HasValue())
		assert.Equal(t, "hello", d.EffectiveValue())
	})

	t.Run("pool only", func(t *testing.T) {
		d := &InputDefault{Pool: []any{"a", "b"}}
		assert.False(t, d.IsLiteralOnly(), "pool is not literal-only")
		assert.True(t, d.HasValue(), "pool counts as having a value")
		assert.Nil(t, d.EffectiveValue(), "pool does not have a single effective value")
	})

	t.Run("from only", func(t *testing.T) {
		d := &InputDefault{From: "step1.output"}
		assert.False(t, d.IsLiteralOnly())
		assert.True(t, d.HasValue())
		assert.Nil(t, d.EffectiveValue())
	})

	t.Run("fromResolved only", func(t *testing.T) {
		d := &InputDefault{FromResolved: "leg1Destination"}
		assert.False(t, d.IsLiteralOnly())
		assert.True(t, d.HasValue())
		assert.Nil(t, d.EffectiveValue())
	})

	t.Run("value with constraint is not literal-only", func(t *testing.T) {
		d := &InputDefault{Value: "ADT", Constraint: "value != 'CHD'"}
		assert.False(t, d.IsLiteralOnly(), "constraint disqualifies literal-only")
		assert.True(t, d.HasValue())
		assert.Nil(t, d.EffectiveValue(), "constrained value has no single effective value")
	})

	t.Run("value with select is not literal-only", func(t *testing.T) {
		d := &InputDefault{
			Value:  "ignored",
			Select: &InputDefaultSelect{Strategy: "first"},
		}
		assert.False(t, d.IsLiteralOnly())
	})

	t.Run("LiteralDefault nil returns nil", func(t *testing.T) {
		assert.Nil(t, LiteralDefault(nil))
	})

	t.Run("LiteralDefault int", func(t *testing.T) {
		d := LiteralDefault(42)
		require.NotNil(t, d)
		assert.Equal(t, 42, d.Value)
		assert.True(t, d.IsLiteralOnly())
	})
}

// --- AfterSpec ---

func TestAfterSpec_UnmarshalYAML_Scalar(t *testing.T) {
	var wf Workflow
	err := yaml.Unmarshal([]byte("name: test\nafter: book\n"), &wf)
	require.NoError(t, err)
	assert.Equal(t, AfterSpec{"book"}, wf.After)
}

func TestAfterSpec_UnmarshalYAML_List(t *testing.T) {
	var wf Workflow
	err := yaml.Unmarshal([]byte("name: test\nafter: [priceOfferFullPayload, priceOfferReference]\n"), &wf)
	require.NoError(t, err)
	assert.Equal(t, AfterSpec{"priceOfferFullPayload", "priceOfferReference"}, wf.After)
}

func TestAfterSpec_UnmarshalYAML_Empty(t *testing.T) {
	var wf Workflow
	err := yaml.Unmarshal([]byte("name: test\n"), &wf)
	require.NoError(t, err)
	assert.False(t, wf.After.IsSet())
}

func TestAfterSpec_MarshalYAML_Scalar(t *testing.T) {
	wf := Workflow{Name: "test", After: AfterSpec{"book"}}
	data, err := yaml.Marshal(&wf)
	require.NoError(t, err)

	var got Workflow
	err = yaml.Unmarshal(data, &got)
	require.NoError(t, err)
	assert.Equal(t, AfterSpec{"book"}, got.After)
}

func TestAfterSpec_MarshalYAML_List(t *testing.T) {
	wf := Workflow{Name: "test", After: AfterSpec{"nodeA", "nodeB"}}
	data, err := yaml.Marshal(&wf)
	require.NoError(t, err)

	var got Workflow
	err = yaml.Unmarshal(data, &got)
	require.NoError(t, err)
	assert.Equal(t, AfterSpec{"nodeA", "nodeB"}, got.After)
}

func TestAfterSpec_Helpers(t *testing.T) {
	empty := AfterSpec{}
	single := AfterSpec{"book"}
	multi := AfterSpec{"priceOfferFullPayload", "priceOfferReference"}

	assert.False(t, empty.IsSet())
	assert.True(t, single.IsSet())
	assert.True(t, multi.IsSet())

	assert.Equal(t, "", empty.First())
	assert.Equal(t, "book", single.First())
	assert.Equal(t, "priceOfferFullPayload", multi.First())

	assert.False(t, empty.Contains("book"))
	assert.True(t, single.Contains("book"))
	assert.False(t, single.Contains("other"))
	assert.True(t, multi.Contains("priceOfferReference"))
	assert.False(t, multi.Contains("book"))

	assert.Equal(t, "book", single.String())
	assert.Equal(t, "priceOfferFullPayload, priceOfferReference", multi.String())
}

func TestInputDefault_RoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		input   Input
		check   func(t *testing.T, got Input)
	}{
		{
			name: "literal string round-trip",
			input: Input{
				Name:    "origin",
				Type:    "string",
				Default: LiteralDefault("JFK"),
			},
			check: func(t *testing.T, got Input) {
				require.NotNil(t, got.Default)
				assert.Equal(t, "JFK", got.Default.Value)
				assert.True(t, got.Default.IsLiteralOnly())
			},
		},
		{
			name: "literal int round-trip",
			input: Input{
				Name:    "count",
				Type:    "integer",
				Default: LiteralDefault(3),
			},
			check: func(t *testing.T, got Input) {
				require.NotNil(t, got.Default)
				assert.Equal(t, 3, got.Default.Value)
			},
		},
		{
			name: "pool round-trip",
			input: Input{
				Name: "airport",
				Type: "airportCode",
				Default: &InputDefault{
					Pool: []any{"DEN", "ORD", "ATL"},
				},
			},
			check: func(t *testing.T, got Input) {
				require.NotNil(t, got.Default)
				assert.Equal(t, []any{"DEN", "ORD", "ATL"}, got.Default.Pool)
				assert.Nil(t, got.Default.Value)
			},
		},
		{
			name: "pool with constraint round-trip",
			input: Input{
				Name: "airport",
				Type: "airportCode",
				Default: &InputDefault{
					Pool:       []any{"DEN", "ORD"},
					Constraint: "value != destination",
				},
			},
			check: func(t *testing.T, got Input) {
				require.NotNil(t, got.Default)
				assert.Equal(t, []any{"DEN", "ORD"}, got.Default.Pool)
				assert.Equal(t, "value != destination", got.Default.Constraint)
			},
		},
		{
			name: "from reference round-trip",
			input: Input{
				Name: "workbenchId",
				Type: "string",
				Default: &InputDefault{
					From: "createWorkbench.workbenchId",
				},
			},
			check: func(t *testing.T, got Input) {
				require.NotNil(t, got.Default)
				assert.Equal(t, "createWorkbench.workbenchId", got.Default.From)
			},
		},
		{
			name: "from with select round-trip",
			input: Input{
				Name: "offeringId",
				Type: "string",
				Default: &InputDefault{
					From: "searchFlights.offerings",
					Select: &InputDefaultSelect{
						Strategy: "first",
						Field:    "offeringId",
					},
				},
			},
			check: func(t *testing.T, got Input) {
				require.NotNil(t, got.Default)
				assert.Equal(t, "searchFlights.offerings", got.Default.From)
				require.NotNil(t, got.Default.Select)
				assert.Equal(t, "first", got.Default.Select.Strategy)
				assert.Equal(t, "offeringId", got.Default.Select.Field)
			},
		},
		{
			name: "fromResolved round-trip",
			input: Input{
				Name: "destination",
				Type: "airportCode",
				Default: &InputDefault{
					FromResolved: "leg1Destination",
				},
			},
			check: func(t *testing.T, got Input) {
				require.NotNil(t, got.Default)
				assert.Equal(t, "leg1Destination", got.Default.FromResolved)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(&tt.input)
			require.NoError(t, err, "marshal should succeed")

			var got Input
			err = yaml.Unmarshal(data, &got)
			require.NoError(t, err, "unmarshal should succeed")

			assert.Equal(t, tt.input.Name, got.Name)
			assert.Equal(t, tt.input.Type, got.Type)
			tt.check(t, got)
		})
	}
}
