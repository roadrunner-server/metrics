package helpers

import (
	"net"
	"net/rpc"
	"testing"

	goridgeRpc "github.com/roadrunner-server/goridge/v4/pkg/rpc"
	"github.com/stretchr/testify/require"
)

// RPC dials the RoadRunner rpc plugin at addr. The client, and with it the
// connection, is closed by t.Cleanup.
func RPC(t *testing.T, addr string) *rpc.Client {
	t.Helper()

	var d net.Dialer
	conn, err := d.DialContext(t.Context(), "tcp", addr)
	require.NoError(t, err)

	client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
	t.Cleanup(func() { _ = client.Close() })

	return client
}
