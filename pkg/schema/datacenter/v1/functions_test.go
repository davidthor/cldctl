package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestStartsWithFunc(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		prefix   string
		expected bool
	}{
		{
			name:     "matching prefix",
			str:      "preview-123",
			prefix:   "preview-",
			expected: true,
		},
		{
			name:     "non-matching prefix",
			str:      "production",
			prefix:   "preview-",
			expected: false,
		},
		{
			name:     "empty prefix matches everything",
			str:      "production",
			prefix:   "",
			expected: true,
		},
		{
			name:     "empty string with empty prefix",
			str:      "",
			prefix:   "",
			expected: true,
		},
		{
			name:     "empty string with non-empty prefix",
			str:      "",
			prefix:   "preview-",
			expected: false,
		},
		{
			name:     "exact match",
			str:      "staging",
			prefix:   "staging",
			expected: true,
		},
		{
			name:     "prefix longer than string",
			str:      "pre",
			prefix:   "preview-",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := startsWithFunc.Call([]cty.Value{
				cty.StringVal(tt.str),
				cty.StringVal(tt.prefix),
			})
			require.NoError(t, err)
			assert.Equal(t, cty.BoolVal(tt.expected), result)
		})
	}
}

func TestEndsWithFunc(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		suffix   string
		expected bool
	}{
		{
			name:     "matching suffix",
			str:      "app.example.com",
			suffix:   ".com",
			expected: true,
		},
		{
			name:     "non-matching suffix",
			str:      "app.example.com",
			suffix:   ".org",
			expected: false,
		},
		{
			name:     "empty suffix matches everything",
			str:      "production",
			suffix:   "",
			expected: true,
		},
		{
			name:     "empty string with empty suffix",
			str:      "",
			suffix:   "",
			expected: true,
		},
		{
			name:     "empty string with non-empty suffix",
			str:      "",
			suffix:   ".com",
			expected: false,
		},
		{
			name:     "exact match",
			str:      "staging",
			suffix:   "staging",
			expected: true,
		},
		{
			name:     "suffix longer than string",
			str:      "com",
			suffix:   ".example.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := endsWithFunc.Call([]cty.Value{
				cty.StringVal(tt.str),
				cty.StringVal(tt.suffix),
			})
			require.NoError(t, err)
			assert.Equal(t, cty.BoolVal(tt.expected), result)
		})
	}
}

func TestStartsWithFunc_RegisteredInStandardFunctions(t *testing.T) {
	fns := standardFunctions()
	_, ok := fns["startswith"]
	assert.True(t, ok, "startswith should be registered in standardFunctions")
}

func TestEndsWithFunc_RegisteredInStandardFunctions(t *testing.T) {
	fns := standardFunctions()
	_, ok := fns["endswith"]
	assert.True(t, ok, "endswith should be registered in standardFunctions")
}
