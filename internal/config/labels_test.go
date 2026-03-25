package config

import (
	"testing"
)

func TestContainerLabels_MinReadySeconds_RoundTrip(t *testing.T) {
	tests := []struct {
		name            string
		minReadySeconds int
		expectInLabels  bool
	}{
		{
			name:            "zero value not emitted",
			minReadySeconds: 0,
			expectInLabels:  false,
		},
		{
			name:            "positive value emitted and parsed",
			minReadySeconds: 10,
			expectInLabels:  true,
		},
		{
			name:            "large value emitted and parsed",
			minReadySeconds: 600,
			expectInLabels:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := &ContainerLabels{
				AppName:         "test-app",
				DeploymentID:    "deploy-1",
				HealthCheckPath: "/health",
				Port:            "8080",
				MinReadySeconds: tt.minReadySeconds,
				Domains: []Domain{
					{Canonical: "example.com"},
				},
			}

			labels := cl.ToLabels()

			if tt.expectInLabels {
				if _, ok := labels[LabelMinReadySeconds]; !ok {
					t.Errorf("expected label %s to be present", LabelMinReadySeconds)
				}
			} else {
				if _, ok := labels[LabelMinReadySeconds]; ok {
					t.Errorf("expected label %s to be absent for zero value", LabelMinReadySeconds)
				}
			}

			parsed, err := ParseContainerLabels(labels)
			if err != nil {
				t.Fatalf("ParseContainerLabels() unexpected error = %v", err)
			}

			if parsed.MinReadySeconds != tt.minReadySeconds {
				t.Errorf("round-trip MinReadySeconds = %d, want %d", parsed.MinReadySeconds, tt.minReadySeconds)
			}
		})
	}
}
