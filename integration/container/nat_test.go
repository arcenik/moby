package container // import "github.com/docker/docker/integration/container"

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/integration/internal/container"
	"github.com/docker/go-connections/nat"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/poll"
	"gotest.tools/v3/skip"
)

func TestNetworkNat(t *testing.T) {
	skip.If(t, testEnv.DaemonInfo.OSType == "windows", "FIXME")
	skip.If(t, testEnv.IsRemoteDaemon)

	ctx := setupTest(t)

	msg := "it works"
	startServerContainer(ctx, t, msg, 8080)

	endpoint := getExternalAddress(t)
	conn, err := net.Dial("tcp", net.JoinHostPort(endpoint.String(), "8080"))
	assert.NilError(t, err)
	defer conn.Close()

	data, err := io.ReadAll(conn)
	assert.NilError(t, err)
	assert.Check(t, is.Equal(msg, strings.TrimSpace(string(data))))
}

func TestNetworkLocalhostTCPNat(t *testing.T) {
	skip.If(t, testEnv.IsRemoteDaemon)
	skip.If(t, testEnv.GitHubActions, "FIXME: https://github.com/moby/moby/issues/41561")

	ctx := setupTest(t)

	msg := "hi yall"
	startServerContainer(ctx, t, msg, 8081)

	conn, err := net.Dial("tcp", "localhost:8081")
	assert.NilError(t, err)
	defer conn.Close()

	data, err := io.ReadAll(conn)
	assert.NilError(t, err)
	assert.Check(t, is.Equal(msg, strings.TrimSpace(string(data))))
}

func TestNetworkLoopbackNat(t *testing.T) {
	skip.If(t, testEnv.GitHubActions, "FIXME: https://github.com/moby/moby/issues/41561")
	skip.If(t, testEnv.DaemonInfo.OSType == "windows", "FIXME")
	skip.If(t, testEnv.IsRemoteDaemon)

	ctx := setupTest(t)

	msg := "it works"
	serverContainerID := startServerContainer(ctx, t, msg, 8080)

	endpoint := getExternalAddress(t)

	apiClient := testEnv.APIClient()

	cID := container.Run(ctx, t, apiClient,
		container.WithCmd("sh", "-c", fmt.Sprintf("stty raw && nc -w 1 %s 8080", endpoint.String())),
		container.WithTty(true),
		container.WithNetworkMode("container:"+serverContainerID),
	)

	poll.WaitOn(t, container.IsStopped(ctx, apiClient, cID), poll.WithDelay(100*time.Millisecond))

	body, err := apiClient.ContainerLogs(ctx, cID, containertypes.LogsOptions{
		ShowStdout: true,
	})
	assert.NilError(t, err)
	defer body.Close()

	var b bytes.Buffer
	_, err = io.Copy(&b, body)
	assert.NilError(t, err)

	assert.Check(t, is.Equal(msg, strings.TrimSpace(b.String())))
}

func startServerContainer(ctx context.Context, t *testing.T, msg string, port int) string {
	t.Helper()
	apiClient := testEnv.APIClient()

	return container.Run(ctx, t, apiClient,
		container.WithName("server-"+t.Name()),
		container.WithCmd("sh", "-c", fmt.Sprintf("echo %q | nc -lp %d", msg, port)),
		container.WithExposedPorts(fmt.Sprintf("%d/tcp", port)),
		func(c *container.TestContainerConfig) {
			c.HostConfig.PortBindings = nat.PortMap{
				nat.Port(fmt.Sprintf("%d/tcp", port)): []nat.PortBinding{
					{
						HostPort: fmt.Sprintf("%d", port),
					},
				},
			}
		},
	)
}

// getExternalAddress() returns the external IP-address from eth0. If eth0 has
// multiple IP-addresses, it returns the first IPv4 IP-address; if no IPv4
// address is present, it returns the first IP-address found.
func getExternalAddress(t *testing.T) net.IP {
	t.Helper()
	iface, err := net.InterfaceByName("eth0")
	skip.If(t, err != nil, "Test not running with `make test-integration`. Interface eth0 not found: %s", err)

	ifaceAddrs, err := iface.Addrs()
	assert.NilError(t, err)
	assert.Check(t, 0 != len(ifaceAddrs))

	if len(ifaceAddrs) > 1 {
		// Prefer IPv4 address if multiple addresses found, as rootlesskit
		// does not handle IPv6 currently https://github.com/moby/moby/pull/41908#issuecomment-774200001
		for _, a := range ifaceAddrs {
			ifaceIP, _, err := net.ParseCIDR(a.String())
			assert.NilError(t, err)
			if ifaceIP.To4() != nil {
				return ifaceIP
			}
		}
	}
	ifaceIP, _, err := net.ParseCIDR(ifaceAddrs[0].String())
	assert.NilError(t, err)

	return ifaceIP
}
