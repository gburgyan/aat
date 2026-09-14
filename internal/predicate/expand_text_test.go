package predicate

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandText(t *testing.T) {
	expand := func(literal string) (any, bool, error) {
		switch literal {
		case "{{amount}}":
			return 1000, true, nil
		case "{{status}}":
			return "succeeded", true, nil
		case "{{refunded}}":
			return true, true, nil
		case "{{bad}}":
			return nil, false, errors.New("no input bad")
		}
		return nil, false, nil
	}

	got, err := ExpandText(`status == "{{status}}" && amountReceived=="{{amount}}" && !(refunded == '{{refunded}}') && currency in ["usd", 'e"ur']`, expand)
	require.NoError(t, err)
	assert.Equal(t, `status == "succeeded" && amountReceived == 1000 && !(refunded == true) && currency in ["usd", 'e"ur']`, got)

	_, err = ExpandText(`a == "{{bad}}"`, expand)
	assert.EqualError(t, err, "no input bad")

	_, err = ExpandText(`a == "open`, expand)
	assert.ErrorContains(t, err, "unterminated string")
}
