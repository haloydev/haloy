package proxy

import (
	"testing"
)

func TestRouteBuilder_AddRoute(t *testing.T) {
	tests := []struct {
		name      string
		canonical string
		aliases   []string
		backends  []Backend
	}{
		{
			name:      "simple route with one backend",
			canonical: "example.com",
			aliases:   nil,
			backends:  []Backend{{IP: "10.0.0.1", Port: "8080"}},
		},
		{
			name:      "route with multiple backends",
			canonical: "example.com",
			aliases:   nil,
			backends:  []Backend{{IP: "10.0.0.1", Port: "8080"}, {IP: "10.0.0.2", Port: "8080"}},
		},
		{
			name:      "route with aliases",
			canonical: "example.com",
			aliases:   []string{"www.example.com", "app.example.com"},
			backends:  []Backend{{IP: "10.0.0.1", Port: "8080"}},
		},
		{
			name:      "case normalization",
			canonical: "EXAMPLE.COM",
			aliases:   []string{"WWW.EXAMPLE.COM"},
			backends:  []Backend{{IP: "10.0.0.1", Port: "8080"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := NewRouteBuilder()
			rb.AddRoute(tt.canonical, tt.aliases, tt.backends)

			config, err := rb.Build()
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}

			// Check route exists with lowercased canonical
			loweredCanonical := "example.com"
			route, ok := config.Routes[loweredCanonical]
			if !ok {
				t.Fatalf("route not found for canonical %q", loweredCanonical)
			}

			if route.Canonical != loweredCanonical {
				t.Errorf("Canonical = %q, want %q", route.Canonical, loweredCanonical)
			}

			if len(route.Backends) != len(tt.backends) {
				t.Errorf("len(Backends) = %d, want %d", len(route.Backends), len(tt.backends))
			}

			// Check aliases are lowercased
			for _, alias := range route.Aliases {
				for i := range alias {
					if alias[i] >= 'A' && alias[i] <= 'Z' {
						t.Errorf("alias %q contains uppercase characters", alias)
						break
					}
				}
			}
		})
	}
}

func TestRouteBuilder_Build_DuplicateAlias(t *testing.T) {
	rb := NewRouteBuilder()
	rb.AddRoute("app1.example.com", []string{"shared.example.com"}, []Backend{{IP: "10.0.0.1", Port: "8080"}})
	rb.AddRoute("app2.example.com", []string{"shared.example.com"}, []Backend{{IP: "10.0.0.2", Port: "8080"}})

	_, err := rb.Build()
	if err == nil {
		t.Fatal("Build() expected error for duplicate alias, got nil")
	}
}

func TestRouteBuilder_Build_CanonicalAliasConflict(t *testing.T) {
	rb := NewRouteBuilder()
	rb.AddRoute("app1.example.com", []string{"app2.example.com"}, []Backend{{IP: "10.0.0.1", Port: "8080"}})
	rb.AddRoute("app2.example.com", nil, []Backend{{IP: "10.0.0.2", Port: "8080"}})

	_, err := rb.Build()
	if err == nil {
		t.Fatal("Build() expected error for canonical/alias conflict, got nil")
	}
}

func TestRouteBuilder_SetAPIDomain(t *testing.T) {
	rb := NewRouteBuilder()
	rb.SetAPIDomain("API.EXAMPLE.COM")

	config, err := rb.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if config.APIDomain != "api.example.com" {
		t.Errorf("APIDomain = %q, want %q", config.APIDomain, "api.example.com")
	}
}

func TestBuildConfigFromDeployments(t *testing.T) {
	deployments := []DeploymentInfo{
		{
			AppName:   "app1",
			Canonical: "app1.example.com",
			Aliases:   []string{"www.app1.example.com"},
			Instances: []InstanceInfo{{IP: "10.0.0.1", Port: "8080"}},
		},
		{
			AppName:   "app2",
			Canonical: "app2.example.com",
			Aliases:   nil,
			Instances: []InstanceInfo{{IP: "10.0.0.2", Port: "8080"}, {IP: "10.0.0.3", Port: "8080"}},
		},
	}

	config, err := BuildConfigFromDeployments(deployments, "api.example.com")
	if err != nil {
		t.Fatalf("BuildConfigFromDeployments() error = %v", err)
	}

	if len(config.Routes) != 2 {
		t.Errorf("len(Routes) = %d, want 2", len(config.Routes))
	}

	if config.APIDomain != "api.example.com" {
		t.Errorf("APIDomain = %q, want %q", config.APIDomain, "api.example.com")
	}

	route1 := config.Routes["app1.example.com"]
	if route1 == nil {
		t.Fatal("route for app1.example.com not found")
	}
	if len(route1.Aliases) != 1 {
		t.Errorf("app1 aliases = %d, want 1", len(route1.Aliases))
	}
	if len(route1.Backends) != 1 {
		t.Errorf("app1 backends = %d, want 1", len(route1.Backends))
	}

	route2 := config.Routes["app2.example.com"]
	if route2 == nil {
		t.Fatal("route for app2.example.com not found")
	}
	if len(route2.Backends) != 2 {
		t.Errorf("app2 backends = %d, want 2", len(route2.Backends))
	}
}

func TestBuildConfigFromDeployments_SkipsEmptyCanonical(t *testing.T) {
	deployments := []DeploymentInfo{
		{
			AppName:   "app-without-domain",
			Canonical: "",
			Instances: []InstanceInfo{{IP: "10.0.0.1", Port: "8080"}},
		},
		{
			AppName:   "app-with-domain",
			Canonical: "app.example.com",
			Instances: []InstanceInfo{{IP: "10.0.0.2", Port: "8080"}},
		},
	}

	config, err := BuildConfigFromDeployments(deployments, "")
	if err != nil {
		t.Fatalf("BuildConfigFromDeployments() error = %v", err)
	}

	if len(config.Routes) != 1 {
		t.Errorf("len(Routes) = %d, want 1 (should skip empty canonical)", len(config.Routes))
	}
}

func TestBuildConfigFromHaloydDeployments(t *testing.T) {
	deployments := map[string]HaloydDeployment{
		"app1": {
			AppName: "app1",
			Domains: []HaloydDomain{
				{Canonical: "app1.example.com", Aliases: []string{"www.app1.example.com"}},
			},
			Instances: []HaloydInstance{{IP: "10.0.0.1", Port: "8080"}},
		},
		"app2": {
			AppName: "app2",
			Domains: []HaloydDomain{
				{Canonical: "app2.example.com", Aliases: nil},
				{Canonical: "other.example.com", Aliases: nil},
			},
			Instances: []HaloydInstance{{IP: "10.0.0.2", Port: "8080"}},
		},
	}

	config, err := BuildConfigFromHaloydDeployments(deployments, "api.example.com")
	if err != nil {
		t.Fatalf("BuildConfigFromHaloydDeployments() error = %v", err)
	}

	// app1 has 1 domain, app2 has 2 domains
	if len(config.Routes) != 3 {
		t.Errorf("len(Routes) = %d, want 3", len(config.Routes))
	}

	if config.APIDomain != "api.example.com" {
		t.Errorf("APIDomain = %q, want %q", config.APIDomain, "api.example.com")
	}
}

func TestBuildConfigFromHaloydDeployments_SkipsEmptyCanonical(t *testing.T) {
	deployments := map[string]HaloydDeployment{
		"app1": {
			AppName: "app1",
			Domains: []HaloydDomain{
				{Canonical: "", Aliases: nil},
				{Canonical: "app1.example.com", Aliases: nil},
			},
			Instances: []HaloydInstance{{IP: "10.0.0.1", Port: "8080"}},
		},
	}

	config, err := BuildConfigFromHaloydDeployments(deployments, "")
	if err != nil {
		t.Fatalf("BuildConfigFromHaloydDeployments() error = %v", err)
	}

	if len(config.Routes) != 1 {
		t.Errorf("len(Routes) = %d, want 1 (should skip empty canonical)", len(config.Routes))
	}
}
