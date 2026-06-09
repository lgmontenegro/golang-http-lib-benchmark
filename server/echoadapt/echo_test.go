package echoadapt

import (
	"testing"

	"example.com/httpdi/server/internal/servertest"
)

func TestIntegration(t *testing.T) {
	servertest.RunSuite(t, New(), ":18084")
}
