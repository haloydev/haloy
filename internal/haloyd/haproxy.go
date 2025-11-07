package haloyd

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/embed"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type HAProxyManager struct {
	cli          *client.Client
	haloydConfig *config.HaloydConfig
	configDir    string
	debug        bool
	updateMutex  sync.Mutex // Mutex protects config writing and reload signaling
}

func NewHAProxyManager(cli *client.Client, haloydConfig *config.HaloydConfig, configDir string, debug bool) *HAProxyManager {
	return &HAProxyManager{
		cli:          cli,
		haloydConfig: haloydConfig,
		configDir:    configDir,
		debug:        debug,
	}
}

// ApplyConfig generates, writes (if not debug), and reloads HAProxy config.
// This method is concurrency-safe due to the internal mutex.
func (hpm *HAProxyManager) ApplyConfig(ctx context.Context, logger *slog.Logger, deployments map[string]Deployment) error {
	logger.Debug("HAProxyManager: Attempting to apply new configuration...")

	hpm.updateMutex.Lock()
	defer hpm.updateMutex.Unlock()

	// Generate Config (with certificate check)
	logger.Debug("HAProxyManager: Generating new configuration...")
	configBuf, err := hpm.generateConfig(deployments)
	if err != nil {
		return fmt.Errorf("HAProxyManager: failed to generate config: %w", err)
	}

	if hpm.debug {
		logger.Debug("HAProxyManager: Skipping config write and reload.")
		logger.Debug(configBuf.String())
		return nil
	}

	configPath := filepath.Join(hpm.configDir, constants.HAProxyConfigFileName)
	logger.Debug("HAProxyManager: Writing config")
	if err := os.WriteFile(configPath, configBuf.Bytes(), constants.ModeFileDefault); err != nil {
		return fmt.Errorf("HAProxyManager: failed to write config file %s: %w", configPath, err)
	}

	haproxyID, err := hpm.getContainerID(ctx, logger)
	if err != nil {
		return fmt.Errorf("HAProxyManager: failed to find HAProxy container: %w", err)
	}
	if haproxyID == "" {
		logger.Warn("HAProxyManager: No HAProxy container found with label, cannot reload.")
		return nil // Not necessarily an error if HAProxy isn't running
	}

	// Signal HAProxy Reload
	logger.Debug("HAProxyManager: Sending SIGUSR2 signal to HAProxy container...")
	err = hpm.cli.ContainerKill(ctx, haproxyID, "SIGUSR2")
	if err != nil {
		return fmt.Errorf("HAProxyManager: failed to send SIGUSR2 to HAProxy container %s: %w", helpers.SafeIDPrefix(haproxyID), err)
	}

	return nil
}

// generateConfig creates the HAProxy configuration content based on deployments.
// It checks for certificate existence before adding HTTPS bindings.
func (hpm *HAProxyManager) generateConfig(deployments map[string]Deployment) (bytes.Buffer, error) {
	var buf bytes.Buffer
	var httpFrontend string
	var httpsFrontend string
	var httpsFrontendUseBackend string
	var backends string
	const indent = "    "

	// Add ACLs for api
	if hpm.haloydConfig != nil && hpm.haloydConfig.API.Domain != "" {
		apiDomain := hpm.haloydConfig.API.Domain
		apiACLName := generateACLName("haloy_api", apiDomain, "acl")

		httpsFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, apiACLName, apiDomain)
		httpsFrontendUseBackend += fmt.Sprintf("%suse_backend haloy_api if %s\n", indent, apiACLName)

		httpFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, apiACLName, apiDomain)
		httpFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s !is_acme_challenge\n",
			indent, apiDomain, apiACLName)

		backends += "backend haloy_api\n"
		backends += fmt.Sprintf("%smode http\n", indent)
		backends += fmt.Sprintf("%s# Forward to the haloyd API server\n", indent)
		backends += fmt.Sprintf("%shttp-request set-header X-Forwarded-For %%[src]\n", indent)
		backends += fmt.Sprintf("%shttp-request set-header X-Forwarded-Proto https\n", indent)
		backends += fmt.Sprintf("%shttp-request set-header X-Forwarded-Port %%[dst_port]\n", indent)
		backends += fmt.Sprintf("%shttp-request set-header Host %%[req.hdr(host)]\n", indent)
		backends += fmt.Sprintf("%sserver haloyd haloyd:%s check\n", indent, constants.APIServerPort)
		backends += "\n"
	}

	for appName, d := range deployments {
		var canonicalACLs []string

		if len(d.Labels.Domains) == 0 {
			continue
		}

		for _, domain := range d.Labels.Domains {
			if domain.Canonical != "" {
				canonicalACLName := generateACLName(appName, domain.Canonical, "canonical")

				httpsFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, canonicalACLName, domain.Canonical)
				canonicalACLs = append(canonicalACLs, canonicalACLName)

				httpFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, canonicalACLName, domain.Canonical)
				// Redirect HTTP to HTTPS for the canonical domain but exclude ACME challenge.
				httpFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s !is_acme_challenge\n",
					indent, domain.Canonical, canonicalACLName)

				for _, alias := range domain.Aliases {
					if alias != "" {
						aliasKey := strings.ReplaceAll(alias, ".", "_")
						aliasACLName := fmt.Sprintf("%s_%s_alias", appName, aliasKey)

						httpsFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, aliasACLName, alias)
						httpsFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s !is_acme_challenge\n",
							indent, domain.Canonical, aliasACLName)

						httpFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, aliasACLName, alias)
						httpFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s !is_acme_challenge\n",
							indent, domain.Canonical, aliasACLName)
					}
				}
			}
		}

		if len(canonicalACLs) > 0 {
			httpsFrontendUseBackend += fmt.Sprintf("%suse_backend %s if %s\n", indent, appName, strings.Join(canonicalACLs, " or "))
		}
	}

	for _, d := range deployments {
		backendName := d.Labels.AppName
		backends += fmt.Sprintf("backend %s\n", backendName)
		for i, instance := range d.Instances {
			backends += fmt.Sprintf("%sserver app%d %s:%s check\n", indent, i+1, instance.IP, instance.Port)
		}
	}

	data, err := embed.TemplatesFS.ReadFile(fmt.Sprintf("templates/%s", constants.HAProxyConfigFileName))
	if err != nil {
		return buf, fmt.Errorf("failed to read embedded file: %w", err)
	}

	tmpl, err := template.New("config").Parse(string(data))
	if err != nil {
		return buf, fmt.Errorf("failed to parse template: %w", err)
	}

	templateData := embed.HAProxyTemplateData{
		HTTPFrontend:            httpFrontend,
		HTTPSFrontend:           httpsFrontend,
		HTTPSFrontendUseBackend: httpsFrontendUseBackend,
		Backends:                backends,
	}

	if err := tmpl.Execute(&buf, templateData); err != nil {
		return buf, fmt.Errorf("failed to execute template: %w", err)
	}

	return buf, nil
}

func (hpm *HAProxyManager) getContainerID(ctx context.Context, logger *slog.Logger) (string, error) {
	maxRetries := 30
	retryInterval := time.Second

	for retry := range maxRetries {
		if ctx.Err() != nil {
			return "", fmt.Errorf("context canceled while waiting for HAProxy container: %w", ctx.Err())
		}

		filtersArgs := filters.NewArgs()
		filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelRole, config.HAProxyLabelRole))
		filtersArgs.Add("status", "running") // Only consider running containers

		containers, err := hpm.cli.ContainerList(ctx, container.ListOptions{
			Filters: filtersArgs,
			Limit:   1, // We only expect one HAProxy container managed by haloy
		})
		if err != nil {
			return "", fmt.Errorf("failed to list containers with label %s=%s: %w",
				config.LabelRole, config.HAProxyLabelRole, err)
		}

		if len(containers) > 0 {
			// Found a running HAProxy container
			return containers[0].ID, nil
		}

		if retry == 1 || retry == maxRetries/2 {
			logger.Info("Waiting for HAProxy container to be running", "attempt", retry+1, "max_retries", maxRetries)
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("context canceled while waiting for HAProxy container: %w", ctx.Err())
		case <-time.After(retryInterval):
			// Continue to next retries
		}
	}

	return "", fmt.Errorf("timed out waiting for HAProxy container to be in running state after %d seconds",
		maxRetries)
}

// sanitizeForACL converts a domain name to a safe ACL identifier
func sanitizeForACL(domain string) string {
	return strings.ReplaceAll(domain, ".", "_")
}

// generateACLName creates a consistent ACL name
func generateACLName(appName, domain, suffix string) string {
	return fmt.Sprintf("%s_%s_%s", appName, sanitizeForACL(domain), suffix)
}
