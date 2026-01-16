package haloy

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/configloader"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func TunnelCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var (
		port        string
		containerID string
	)

	cmd := &cobra.Command{
		Use:   "tunnel <local-port>",
		Short: "Create a TCP tunnel to a container",
		Long: `Create a TCP tunnel to a running container, allowing local connections to be
forwarded to the container's port.

The remote port defaults to the port configured in haloy.yaml. Use --port to override.

Examples:
  # Tunnel to a database container (uses port from config)
  haloy tunnel 15432
  # Then connect: psql -h localhost -p 15432

  # Override the remote port
  haloy tunnel 15432 --port 5432

  # Tunnel to a specific container (for apps with replicas)
  haloy tunnel 15432 --container abc123`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			localPort := args[0]

			if err := helpers.ValidatePort(localPort); err != nil {
				return fmt.Errorf("invalid port: %w", err)
			}

			if port != "" {
				if err := helpers.ValidatePort(port); err != nil {
					return fmt.Errorf("invalid remote port: %w", err)
				}
			}

			rawDeployConfig, format, err := configloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				return fmt.Errorf("unable to load config: %w", err)
			}

			resolvedDeployConfig, err := configloader.ResolveSecrets(ctx, rawDeployConfig)
			if err != nil {
				return fmt.Errorf("unable to resolve secrets: %w", err)
			}

			targets, err := configloader.ExtractTargets(resolvedDeployConfig, format)
			if err != nil {
				return err
			}

			if len(targets) != 1 {
				return fmt.Errorf("tunnel requires exactly one target, got %d (use --targets to specify)", len(targets))
			}

			var target config.TargetConfig
			for _, t := range targets {
				target = t
				break
			}
			return runTunnel(ctx, &target, localPort, port, containerID)
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Target to tunnel to (required for multi-target configs)")
	cmd.Flags().StringVar(&port, "port", "", "Remote port to tunnel to (default: port from config)")
	cmd.Flags().StringVar(&containerID, "container", "", "Specific container ID to tunnel to")

	return cmd
}

func runTunnel(ctx context.Context, targetConfig *config.TargetConfig, localPort, remotePort, containerID string) error {
	token, err := getToken(targetConfig, targetConfig.Server)
	if err != nil {
		return fmt.Errorf("unable to get token: %w", err)
	}

	api, err := apiclient.New(targetConfig.Server, token)
	if err != nil {
		return fmt.Errorf("unable to create API client: %w", err)
	}

	if err := api.HealthCheck(ctx); err != nil {
		return fmt.Errorf("server not available: %w", err)
	}

	normalizedURL, err := helpers.NormalizeServerURL(targetConfig.Server)
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}

	// Determine TLS based on localhost check (same logic as BuildServerURL)
	useTLS := !helpers.IsLocalhost(normalizedURL)

	// Add default port if not specified
	host := normalizedURL
	if !strings.Contains(host, ":") {
		if useTLS {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	// Check if port is already in use
	listenAddr := fmt.Sprintf("localhost:%s", localPort)
	checkConn, err := net.DialTimeout("tcp", listenAddr, 100*time.Millisecond)
	if err == nil {
		checkConn.Close()
		return fmt.Errorf("port %s is already in use", localPort)
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
	}
	defer listener.Close()

	ui.Info("Tunnel listening on localhost:%s -> %s", localPort, targetConfig.Name)
	ui.Info("Press Ctrl+C to stop")

	// Accept connections
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		localConn, err := listener.Accept()
		if err != nil {
			// Check if context was cancelled
			select {
			case <-ctx.Done():
				return nil
			default:
				ui.Warn("Failed to accept connection: %v", err)
				continue
			}
		}

		// Handle each connection in a goroutine
		go func(local net.Conn) {
			defer local.Close()

			// Establish tunnel to server
			remote, err := dialTunnel(host, useTLS, token, targetConfig.Name, remotePort, containerID)
			if err != nil {
				ui.Error("Failed to establish tunnel: %v", err)
				return
			}
			defer remote.Close()

			// Bidirectional copy
			var wg sync.WaitGroup
			wg.Add(2)

			// Local -> Remote
			go func() {
				defer wg.Done()
				io.Copy(remote, local)
				if tc, ok := remote.(*tunnelConn); ok {
					if tcpConn, ok := tc.closer.(*net.TCPConn); ok {
						tcpConn.CloseWrite()
					}
				} else if tcpConn, ok := remote.(*net.TCPConn); ok {
					tcpConn.CloseWrite()
				}
			}()

			// Remote -> Local
			go func() {
				defer wg.Done()
				io.Copy(local, remote)
				if tcpConn, ok := local.(*net.TCPConn); ok {
					tcpConn.CloseWrite()
				}
			}()

			wg.Wait()
		}(localConn)
	}
}

// dialTunnel establishes a TCP tunnel to a container through the API server.
// host should include the port (e.g., "example.com:443").
// It returns a net.Conn that can be used to communicate with the container.
func dialTunnel(host string, useTLS bool, token, appName, port, containerID string) (net.Conn, error) {
	// Build path with query params
	path := fmt.Sprintf("/v1/tunnel/%s", appName)

	params := make([]string, 0)
	if port != "" {
		params = append(params, "port="+port)
	}
	if containerID != "" {
		params = append(params, "container="+containerID)
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	// Establish connection
	var conn net.Conn
	var err error
	if useTLS {
		// Force HTTP/1.1 via ALPN - HTTP/2 doesn't support connection hijacking
		conn, err = tls.Dial("tcp", host, &tls.Config{
			NextProtos: []string{"http/1.1"},
		})
	} else {
		conn, err = net.Dial("tcp", host)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	// Send HTTP request manually with upgrade headers
	// Extract just the hostname (without port) for the Host header
	hostHeader := strings.Split(host, ":")[0]
	httpReq := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\n", path, hostHeader)
	if token != "" {
		httpReq += fmt.Sprintf("Authorization: Bearer %s\r\n", token)
	}
	httpReq += "Connection: Upgrade\r\n"
	httpReq += "Upgrade: tcp\r\n"
	httpReq += "\r\n"

	_, err = conn.Write([]byte(httpReq))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse status code
	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 2 {
		conn.Close()
		return nil, fmt.Errorf("invalid response: %s", statusLine)
	}

	statusCode := 0
	fmt.Sscanf(parts[1], "%d", &statusCode)

	// Read headers until empty line
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to read headers: %w", err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	// Accept both 101 (Switching Protocols) and 200 (Connection Established)
	if statusCode != 101 && statusCode != 200 {
		// Try to read error body
		body, _ := io.ReadAll(reader)
		conn.Close()
		if statusCode == 401 {
			return nil, fmt.Errorf("authentication failed - check your %s", constants.EnvVarAPIToken)
		}
		return nil, fmt.Errorf("tunnel request failed with status %d: %s", statusCode, strings.TrimSpace(string(body)))
	}

	// Check if there's buffered data in the reader
	buffered := reader.Buffered()

	if buffered > 0 {
		// Return a connection that first reads from the buffer, then from the conn
		return &tunnelConn{
			reader:   reader,
			writer:   conn,
			closer:   conn,
			respBody: nil,
		}, nil
	}

	// No buffered data, return the raw connection
	return conn, nil
}

// tunnelConn wraps the HTTP response body for reading and the underlying connection for writing.
// This is necessary because the HTTP client may have buffered data in resp.Body that we need to read.
type tunnelConn struct {
	reader   io.Reader
	writer   io.Writer
	closer   io.Closer
	respBody io.Closer
}

func (t *tunnelConn) Read(p []byte) (int, error) {
	return t.reader.Read(p)
}

func (t *tunnelConn) Write(p []byte) (int, error) {
	return t.writer.Write(p)
}

func (t *tunnelConn) Close() error {
	if t.respBody != nil {
		t.respBody.Close()
	}
	return t.closer.Close()
}

// Implement net.Conn interface methods that we need
func (t *tunnelConn) LocalAddr() net.Addr {
	if conn, ok := t.closer.(net.Conn); ok {
		return conn.LocalAddr()
	}
	return nil
}

func (t *tunnelConn) RemoteAddr() net.Addr {
	if conn, ok := t.closer.(net.Conn); ok {
		return conn.RemoteAddr()
	}
	return nil
}

func (t *tunnelConn) SetDeadline(deadline time.Time) error {
	if conn, ok := t.closer.(net.Conn); ok {
		return conn.SetDeadline(deadline)
	}
	return nil
}

func (t *tunnelConn) SetReadDeadline(deadline time.Time) error {
	if conn, ok := t.closer.(net.Conn); ok {
		return conn.SetReadDeadline(deadline)
	}
	return nil
}

func (t *tunnelConn) SetWriteDeadline(deadline time.Time) error {
	if conn, ok := t.closer.(net.Conn); ok {
		return conn.SetWriteDeadline(deadline)
	}
	return nil
}
