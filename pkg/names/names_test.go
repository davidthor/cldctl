package names

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerate_Deterministic(t *testing.T) {
	// Same inputs should always produce the same name
	name1 := Generate("staging", "my-app", "main")
	name2 := Generate("staging", "my-app", "main")
	assert.Equal(t, name1, name2, "same inputs should produce the same name")
}

func TestGenerate_DifferentInputs(t *testing.T) {
	// Different inputs should produce different names (with very high probability)
	name1 := Generate("staging", "my-app", "main")
	name2 := Generate("production", "my-app", "main")
	name3 := Generate("staging", "other-app", "main")
	name4 := Generate("staging", "my-app", "api")

	assert.NotEqual(t, name1, name2, "different environments should produce different names")
	assert.NotEqual(t, name1, name3, "different components should produce different names")
	assert.NotEqual(t, name1, name4, "different route names should produce different names")
}

func TestGenerate_Format(t *testing.T) {
	name := Generate("staging", "my-app", "main")

	// Should be in "adjective-noun" format
	assert.Regexp(t, `^[a-z]+-[a-z]+$`, name, "name should match adjective-noun format")
}

func TestGenerate_SinglePart(t *testing.T) {
	name := Generate("onlyone")
	assert.Regexp(t, `^[a-z]+-[a-z]+$`, name, "single part should also produce adjective-noun format")
}

func TestGenerate_EmptyParts(t *testing.T) {
	name := Generate()
	assert.Regexp(t, `^[a-z]+-[a-z]+$`, name, "empty parts should still produce a valid name")
}

func TestGenerate_Uniqueness(t *testing.T) {
	// Generate many names and check for reasonable uniqueness
	seen := make(map[string]bool)
	duplicates := 0
	total := 1000

	for i := 0; i < total; i++ {
		name := Generate("env", "comp", string(rune('a'+i%26)), string(rune('0'+i/26)))
		if seen[name] {
			duplicates++
		}
		seen[name] = true
	}

	// With ~250 adjectives * ~250 nouns = ~62,500 combinations,
	// 1000 random picks should have very few collisions
	assert.Less(t, duplicates, 50, "too many duplicates in %d generations", total)
}
