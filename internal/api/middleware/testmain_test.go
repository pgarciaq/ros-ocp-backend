package middleware

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("github.com/hashicorp/golang-lru/v2/expirable.NewLRU[...].func1"),
	)
}
