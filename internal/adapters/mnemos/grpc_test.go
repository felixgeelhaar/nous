package mnemos

import (
	"context"
	"testing"

	"github.com/felixgeelhaar/nous/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestAdapter_NotConfigured(t *testing.T) {
	a, err := NewGRPC("", "", "")
	require.NoError(t, err)
	require.NotNil(t, a)
	defer func() { _ = a.Close() }()

	ctx := context.Background()

	_, err = a.Recall(ctx, ports.RecallQuery{})
	require.ErrorIs(t, err, ports.ErrClientNotConfigured)

	_, err = a.AppendEvent(ctx, ports.MnemosEvent{})
	require.ErrorIs(t, err, ports.ErrClientNotConfigured)
}

func TestAdapter_Close_Unconfigured(t *testing.T) {
	a, err := NewGRPC("", "", "")
	require.NoError(t, err)
	require.NoError(t, a.Close())
}
