package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
