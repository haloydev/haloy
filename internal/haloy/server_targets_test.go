package haloy

import (
	"context"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveServerTargetsAllowsMultiTargetConfigWithOneResolvedServer(t *testing.T) {
	configPath := writeTestConfig(t, `
server: haloy.example.com
api_token:
  value: test-token
targets:
  api:
    domains:
      - domain: api.example.com
  worker:
    domains:
      - domain: worker.example.com
`)
	flags := &appCmdFlags{}
	cmd := newServerTargetsTestCommand(flags)

	servers, err := resolveServerTargets(context.Background(), cmd, configPath, flags)
	if err != nil {
		t.Fatalf("resolveServerTargets error = %v", err)
	}

	if len(servers) != 1 {
		t.Fatalf("server count = %d, want 1", len(servers))
	}
	if servers[0].Server != "haloy.example.com" {
		t.Fatalf("server = %q, want haloy.example.com", servers[0].Server)
	}
	if !reflect.DeepEqual(servers[0].TargetNames, []string{"api", "worker"}) {
		t.Fatalf("target names = %#v, want api/worker", servers[0].TargetNames)
	}
	if servers[0].TargetConfig.APIToken == nil || servers[0].TargetConfig.APIToken.Value != "test-token" {
		t.Fatalf("target API token was not inherited from top-level config")
	}
}

func TestResolveServerTargetsReturnsMultipleServersWithoutSelectors(t *testing.T) {
	configPath := writeTestConfig(t, `
targets:
  api:
    server: haloy-one.example.com
    api_token:
      value: token-one
  worker:
    server: haloy-two.example.com
    api_token:
      value: token-two
`)
	flags := &appCmdFlags{}
	cmd := newServerTargetsTestCommand(flags)

	servers, err := resolveServerTargets(context.Background(), cmd, configPath, flags)
	if err != nil {
		t.Fatalf("resolveServerTargets error = %v", err)
	}

	got := []string{servers[0].Server, servers[1].Server}
	want := []string{"haloy-one.example.com", "haloy-two.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("servers = %#v, want %#v", got, want)
	}
}

func TestResolveServerTargetsFiltersWhenSelectorProvided(t *testing.T) {
	configPath := writeTestConfig(t, `
targets:
  api:
    server: haloy-one.example.com
    api_token:
      value: token-one
  worker:
    server: haloy-two.example.com
    api_token:
      value: token-two
`)
	flags := &appCmdFlags{}
	cmd := newServerTargetsTestCommand(flags)
	if err := cmd.ParseFlags([]string{"--targets", "worker"}); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}

	servers, err := resolveServerTargets(context.Background(), cmd, configPath, flags)
	if err != nil {
		t.Fatalf("resolveServerTargets error = %v", err)
	}

	if len(servers) != 1 {
		t.Fatalf("server count = %d, want 1", len(servers))
	}
	if servers[0].Server != "haloy-two.example.com" {
		t.Fatalf("server = %q, want haloy-two.example.com", servers[0].Server)
	}
	if !reflect.DeepEqual(servers[0].TargetNames, []string{"worker"}) {
		t.Fatalf("target names = %#v, want worker", servers[0].TargetNames)
	}
}

func newServerTargetsTestCommand(flags *appCmdFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "targets")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "all")
	return cmd
}
