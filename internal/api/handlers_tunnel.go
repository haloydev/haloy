package api

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/docker"
)

func (s *APIServer) handleTunnel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.PathValue("appName")
		if appName == "" {
			http.Error(w, "App name is required", http.StatusBadRequest)
			return
		}

		portOverride := r.URL.Query().Get("port")
		containerID := r.URL.Query().Get("container")

		ctx := r.Context()

		cli, err := docker.NewClient(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		containers, err := docker.GetAppContainers(ctx, cli, false, appName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(containers) == 0 {
			http.Error(w, "No running containers found for the specified app", http.StatusNotFound)
			return
		}

		// Determine which container to tunnel to
		var targetContainerID string
		if containerID != "" {
			// Find specific container by ID (supports short IDs)
			found := false
			for _, c := range containers {
				if c.ID == containerID || strings.HasPrefix(c.ID, containerID) {
					targetContainerID = c.ID
					found = true
					break
				}
			}
			if !found {
				http.Error(w, "Specified container not found for this app", http.StatusNotFound)
				return
			}
		} else {
			// Default to first container
			targetContainerID = containers[0].ID
		}

		containerInfo, err := cli.ContainerInspect(ctx, targetContainerID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to inspect container: %v", err), http.StatusInternalServerError)
			return
		}

		// Get container IP - handle host network mode specially
		var containerIP string
		if containerInfo.HostConfig != nil && containerInfo.HostConfig.NetworkMode == "host" {
			containerIP = "127.0.0.1"
		} else {
			containerIP, err = docker.ContainerNetworkIP(containerInfo, constants.DockerNetwork)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to get container IP: %v", err), http.StatusInternalServerError)
				return
			}
		}

		// Get port from labels (or use override)
		labels, err := config.ParseContainerLabels(containerInfo.Config.Labels)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse container labels: %v", err), http.StatusInternalServerError)
			return
		}

		port := labels.Port.String()
		if portOverride != "" {
			port = portOverride
		}
		if port == "" {
			http.Error(w, "No port configured for container", http.StatusBadRequest)
			return
		}

		targetAddr := fmt.Sprintf("%s:%s", containerIP, port)

		// Try connecting to container before hijacking
		dialer := &net.Dialer{}
		containerConn, err := dialer.DialContext(ctx, "tcp", targetAddr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to connect to container at %s: %v", targetAddr, err), http.StatusBadGateway)
			return
		}
		defer containerConn.Close()

		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
			return
		}

		clientConn, bufrw, err := hijacker.Hijack()
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to hijack connection: %v", err), http.StatusInternalServerError)
			return
		}
		defer clientConn.Close()

		// Send 101 Switching Protocols to signal the connection is upgraded to a raw TCP tunnel
		_, err = bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: tcp\r\nConnection: Upgrade\r\n\r\n")
		if err != nil {
			return
		}
		if err = bufrw.Flush(); err != nil {
			return
		}

		// Bidirectional copy
		var wg sync.WaitGroup
		wg.Add(2)

		// Client -> Container
		// Read from bufrw to capture any buffered data from the hijacked connection
		go func() {
			defer wg.Done()
			io.Copy(containerConn, bufrw)
			// Close write side to signal EOF to container
			if tcpConn, ok := containerConn.(*net.TCPConn); ok {
				tcpConn.CloseWrite()
			}
		}()

		// Container -> Client
		go func() {
			defer wg.Done()
			io.Copy(clientConn, containerConn)
			// Close write side to signal EOF to client
			if tcpConn, ok := clientConn.(*net.TCPConn); ok {
				tcpConn.CloseWrite()
			}
		}()

		wg.Wait()
	}
}
