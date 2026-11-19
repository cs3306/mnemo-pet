package rules

import (
	"testing"

	"mnemo-pet/internal/config"
)

func defaultRulesForTest(t *testing.T) []byte {
	t.Helper()
	return config.DefaultRules()
}
