package validate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeJSONPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{name: "plain field", input: "name", expect: "name"},
		{name: "nested field", input: "price.amount", expect: "price.amount"},
		{name: "dollar prefix", input: "$.name", expect: "name"},
		{name: "dollar nested", input: "$.price.amount", expect: "price.amount"},
		{name: "lone dollar", input: "$", expect: "@this"},
		{name: "bracket index", input: "results[0]", expect: "results.0"},
		{name: "bracket nested", input: "results[0].name", expect: "results.0.name"},
		{name: "dollar bracket", input: "$.results[0].name", expect: "results.0.name"},
		{name: "multiple brackets", input: "data[0].items[1].value", expect: "data.0.items.1.value"},
		{name: "no transform needed", input: "simple", expect: "simple"},
		{name: "empty string", input: "", expect: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeJSONPath(tt.input)
			assert.Equal(t, tt.expect, got)
		})
	}
}
