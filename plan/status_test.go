package plan

import (
	"encoding/json"
	"testing"

	"github.com/gburgyan/aat/internal/yamlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestExpectedStatus_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    ExpectedStatuses
		wantErr string
	}{
		{
			name: "HTTP codes",
			yaml: "status: [404, 409]\n",
			want: ExpectedStatuses{{Code: 404}, {Code: 409}},
		},
		{
			name: "gRPC names",
			yaml: "status: [NOT_FOUND, ALREADY_EXISTS]\n",
			want: ExpectedStatuses{{Code: 404, Name: "NOT_FOUND"}, {Code: 409, Name: "ALREADY_EXISTS"}},
		},
		{
			name: "mixed",
			yaml: "status: [404, PERMISSION_DENIED]\n",
			want: ExpectedStatuses{{Code: 404}, {Code: 403, Name: "PERMISSION_DENIED"}},
		},
		{
			name:    "an unknown name lists the codes",
			yaml:    "status: [NOT_FOND]\n",
			wantErr: "unknown status",
		},
		{
			name:    "a mapping is neither form",
			yaml:    "status: [{code: 404}]\n",
			wantErr: "a status is a code such as 404, a class such as 4xx, or a gRPC name",
		},
		{
			name: "classes",
			yaml: "status: [4xx, 5XX, 409]\n",
			want: ExpectedStatuses{{Class: 4}, {Class: 5}, {Code: 409}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var holder struct {
				Status ExpectedStatuses `yaml:"status"`
			}
			err := yamlx.Decode([]byte(tt.yaml), &holder)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, holder.Status)
		})
	}
}

func TestExpectedStatus_Class(t *testing.T) {
	s := ExpectedStatuses{{Class: 4}}
	assert.True(t, s.Matches(400, ""))
	assert.True(t, s.Matches(422, ""))
	assert.True(t, s.Matches(404, "NOT_FOUND"), "a gRPC status matches the class of the HTTP status it maps to")
	assert.False(t, s.Matches(500, ""))
	assert.False(t, s.Matches(200, ""))
	assert.Equal(t, []string{"4xx"}, s.Strings())
	assert.True(t, s[0].IsFailure())
	assert.False(t, ExpectedStatus{Class: 2}.IsFailure(), "2xx is not a failure")

	out, err := yaml.Marshal(ExpectedStatuses{{Class: 4}, {Code: 409}})
	require.NoError(t, err)
	assert.Equal(t, "- 4xx\n- 409\n", string(out))

	data, err := json.Marshal(ExpectedStatuses{{Class: 5}})
	require.NoError(t, err)
	assert.JSONEq(t, `["5xx"]`, string(data))
	var back ExpectedStatuses
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, ExpectedStatuses{{Class: 5}}, back)
}

func TestExpectedStatus_Matches(t *testing.T) {
	t.Run("a number matches the mapped status", func(t *testing.T) {
		s := ExpectedStatuses{{Code: 404}}
		assert.True(t, s.Matches(404, ""), "an HTTP 404")
		assert.True(t, s.Matches(404, "NOT_FOUND"), "a gRPC NOT_FOUND, which maps to 404")
		assert.False(t, s.Matches(409, "ALREADY_EXISTS"))
	})

	t.Run("a name distinguishes codes sharing an HTTP status", func(t *testing.T) {
		// This is why the name is kept: INVALID_ARGUMENT, FAILED_PRECONDITION,
		// and OUT_OF_RANGE all map to 400, so matching on the number alone
		// would let a negative test pass on the wrong failure.
		s := ExpectedStatuses{{Code: 400, Name: "INVALID_ARGUMENT"}}
		assert.True(t, s.Matches(400, "INVALID_ARGUMENT"))
		assert.False(t, s.Matches(400, "FAILED_PRECONDITION"))
		assert.False(t, s.Matches(400, "OUT_OF_RANGE"))
	})

	t.Run("a name still matches an HTTP response by its code", func(t *testing.T) {
		s := ExpectedStatuses{{Code: 404, Name: "NOT_FOUND"}}
		assert.True(t, s.Matches(404, ""), "so a plan reads against either protocol")
	})
}

func TestExpectedStatus_IsFailure(t *testing.T) {
	// OK is the only gRPC code mapping below 400, so one rule serves both.
	assert.False(t, ExpectedStatus{Code: 200}.IsFailure())
	assert.False(t, ExpectedStatus{Code: 200, Name: "OK"}.IsFailure())
	assert.True(t, ExpectedStatus{Code: 404, Name: "NOT_FOUND"}.IsFailure())
	assert.True(t, ExpectedStatus{Code: 503, Name: "UNAVAILABLE"}.IsFailure())
	assert.True(t, ExpectedStatus{Code: 499, Name: "CANCELLED"}.IsFailure())
}

func TestExpectedStatus_RoundTrips(t *testing.T) {
	statuses := ExpectedStatuses{{Code: 404}, {Code: 403, Name: "PERMISSION_DENIED"}}

	t.Run("YAML writes it back as written", func(t *testing.T) {
		out, err := yaml.Marshal(statuses)
		require.NoError(t, err)
		assert.Equal(t, "- 404\n- PERMISSION_DENIED\n", string(out))

		var back ExpectedStatuses
		require.NoError(t, yamlx.Decode(out, &back))
		assert.Equal(t, statuses, back)
	})

	t.Run("JSON keeps numbers numeric, so HTTP archives are unchanged", func(t *testing.T) {
		out, err := json.Marshal(statuses)
		require.NoError(t, err)
		assert.JSONEq(t, `[404,"PERMISSION_DENIED"]`, string(out))

		var back ExpectedStatuses
		require.NoError(t, json.Unmarshal(out, &back))
		assert.Equal(t, statuses, back)
	})
}

func TestExpectedStatus_JSONKeepsAnUnreadableName(t *testing.T) {
	// An archive must stay readable whatever it holds, a redacted name
	// included. It carries no code, so it can only fail to match.
	var s ExpectedStatus
	require.NoError(t, json.Unmarshal([]byte(`"[REDACTED]"`), &s))
	assert.Equal(t, "[REDACTED]", s.Name)
	assert.Equal(t, 0, s.Code)
	assert.False(t, s.IsFailure())
	assert.False(t, ExpectedStatuses{s}.Matches(404, "NOT_FOUND"))
}

func TestExpectedStatuses_Helpers(t *testing.T) {
	s := ExpectedStatuses{{Code: 404}, {Code: 403, Name: "PERMISSION_DENIED"}}
	assert.Equal(t, []int{404, 403}, s.Codes())
	assert.Equal(t, []string{"404", "PERMISSION_DENIED"}, s.Strings())
	assert.Equal(t, ExpectedStatuses{{Code: 404}, {Code: 409}}, HTTPStatuses([]int{404, 409}))
	assert.Nil(t, HTTPStatuses(nil))
}

func TestContradictsFailure(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want bool
	}{
		{"success code", 200, true},
		{"success class", "2xx", true},
		{"failure code", 404, false},
		{"failure class", "4xx", false},
		{"gRPC OK", "OK", true},
		{"gRPC OK, lowercase", "ok", true},
		{"gRPC NOT_FOUND", "NOT_FOUND", false},
		{"gRPC INVALID_ARGUMENT", "INVALID_ARGUMENT", false},
		// CANCELLED maps to 499, a failure, so it agrees with expectFailure.
		{"gRPC CANCELLED", "CANCELLED", false},
		{"unrecognized", "abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ContradictsFailure(tt.v))
		})
	}
}
