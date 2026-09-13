package predicate

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_EvalsManyFieldSets(t *testing.T) {
	p, err := Parse(`status == "open" && total > 100`)
	require.NoError(t, err)

	for _, tt := range []struct {
		fields map[string]any
		want   bool
	}{
		{fields: map[string]any{"status": "open", "total": 150.0}, want: true},
		{fields: map[string]any{"status": "open", "total": 50}, want: false},
		{fields: map[string]any{"status": "closed", "total": 150.0}, want: false},
	} {
		got, err := p.Eval(tt.fields)
		require.NoError(t, err)
		assert.Equal(t, tt.want, got, "%v", tt.fields)
	}

	_, err = p.Eval(map[string]any{"status": "open"})
	assert.ErrorContains(t, err, `unknown field "total"`)

	_, err = Parse(`status ==`)
	assert.ErrorContains(t, err, "parse:")
	_, err = Parse(`status == "open`)
	assert.ErrorContains(t, err, "tokenize:")
}

func TestLiterals(t *testing.T) {
	literals, err := Literals(`sku == "{{sku}}" && status in ['open', "held"] && total > 5`)
	require.NoError(t, err)
	assert.Equal(t, []string{"{{sku}}", "open", "held"}, literals)

	_, err = Literals(`note == "unterminated`)
	assert.ErrorContains(t, err, "tokenize:")
}

func TestEvalExpanding(t *testing.T) {
	expand := func(literal string) (any, bool, error) {
		switch literal {
		case "three":
			return 3, true, nil
		case "yes":
			return true, true, nil
		case "bad":
			return nil, false, errors.New("cannot expand")
		}
		return nil, false, nil
	}

	got, err := EvalExpanding(`quantity == "three" && gift == "yes" && sku == "SKU-1"`,
		map[string]any{"quantity": 3.0, "gift": true, "sku": "SKU-1"}, expand)
	require.NoError(t, err)
	assert.True(t, got, "a number and a boolean compare as values, and a literal expand leaves alone stays text")

	_, err = EvalExpanding(`sku == "bad"`, map[string]any{"sku": "SKU-1"}, expand)
	assert.EqualError(t, err, "cannot expand")
}
