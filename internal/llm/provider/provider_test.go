package provider

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsTransientNetworkError(t *testing.T) {
	assert.True(t, isTransientNetworkError(context.DeadlineExceeded))
	assert.True(t, isTransientNetworkError(io.ErrUnexpectedEOF))
	assert.True(t, isTransientNetworkError(&net.DNSError{IsTimeout: true}))
	assert.True(t, isTransientNetworkError(fmt.Errorf("wrapped: %w", &net.OpError{Op: "read", Err: io.ErrUnexpectedEOF})))
	assert.True(t, isTransientNetworkError(fmt.Errorf("wsarecv: An existing connection was forcibly closed by the remote host")))
	assert.False(t, isTransientNetworkError(fmt.Errorf("invalid api key")))
}

func TestRetryDelayMsCapsAtConfiguredMaximum(t *testing.T) {
	assert.Equal(t, int64((3 * time.Second).Milliseconds()), retryDelayMs(1, nil))
	assert.Equal(t, int64((30 * time.Second).Milliseconds()), retryDelayMs(8, nil))
	assert.Equal(t, int64((17 * time.Second).Milliseconds()), retryDelayMs(3, []string{"17"}))
}

func TestShouldRetryTransientNetworkErrorHonorsRetryBudget(t *testing.T) {
	retry, after, err := shouldRetryTransientNetworkError(1, context.DeadlineExceeded)
	assert.True(t, retry)
	assert.NoError(t, err)
	assert.Equal(t, int64((3 * time.Second).Milliseconds()), after)

	retry, after, err = shouldRetryTransientNetworkError(maxRetries+1, context.DeadlineExceeded)
	assert.False(t, retry)
	assert.Zero(t, after)
	assert.Error(t, err)
}
