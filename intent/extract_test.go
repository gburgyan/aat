package intent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractYAML_YAMLCodeBlock(t *testing.T) {
	response := "Here is the plan:\n\n```yaml\nmetadata:\n  prompt: test\nexecution:\n  steps:\n    - node: searchFlights\n```\n\nLet me know if you need changes."

	data, err := ExtractYAML(response)
	require.NoError(t, err)
	assert.Contains(t, string(data), "metadata:")
	assert.Contains(t, string(data), "searchFlights")
	assert.NotContains(t, string(data), "```")
}

func TestExtractYAML_GenericCodeBlock(t *testing.T) {
	response := "```\nmetadata:\n  prompt: test\nexecution:\n  steps:\n    - node: foo\n```"

	data, err := ExtractYAML(response)
	require.NoError(t, err)
	assert.Contains(t, string(data), "metadata:")
}

func TestExtractYAML_RawYAML(t *testing.T) {
	response := "metadata:\n  prompt: test\nexecution:\n  steps:\n    - node: foo"

	data, err := ExtractYAML(response)
	require.NoError(t, err)
	assert.Contains(t, string(data), "metadata:")
}

func TestExtractYAML_RawYAMLWithDashes(t *testing.T) {
	response := "---\nmetadata:\n  prompt: test"

	data, err := ExtractYAML(response)
	require.NoError(t, err)
	assert.Contains(t, string(data), "metadata:")
}

func TestExtractYAML_GarbageInput(t *testing.T) {
	response := "I'm sorry, I can't help with that request."

	_, err := ExtractYAML(response)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no YAML found")
}

func TestExtractYAML_EmptyResponse(t *testing.T) {
	_, err := ExtractYAML("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
}

func TestExtractYAML_WhitespaceOnly(t *testing.T) {
	_, err := ExtractYAML("   \n  \n  ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
}

func TestExtractYAML_MultipleBlocks_TakesFirst(t *testing.T) {
	response := "```yaml\nfirst: block\n```\n\n```yaml\nsecond: block\n```"

	data, err := ExtractYAML(response)
	require.NoError(t, err)
	assert.Contains(t, string(data), "first: block")
	assert.NotContains(t, string(data), "second: block")
}

func TestExtractYAML_PreferYAMLOverGeneric(t *testing.T) {
	// ```yaml should be found before ``` even if generic appears first in position
	response := "```yaml\nspecific: yaml\n```"

	data, err := ExtractYAML(response)
	require.NoError(t, err)
	assert.Contains(t, string(data), "specific: yaml")
}

func TestExtractYAML_EmptyCodeBlock(t *testing.T) {
	response := "```yaml\n```"

	// Empty block should fall through to next strategy
	_, err := ExtractYAML(response)
	require.Error(t, err)
}
