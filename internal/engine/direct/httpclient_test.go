package direct

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProviderHTTPClientTimeout(t *testing.T) {
	require.Equal(t, 120*time.Second, providerHTTPClient.Timeout)
}
