package init

import (
	"errors"
	"testing"
)

func TestSelectServerFrom_SingleServer(t *testing.T) {
	servers := []string{"api.example.com"}

	// promptFn should NOT be called for single server
	promptFn := func(servers []string) (string, error) {
		t.Error("promptFn should not be called for single server")
		return "", nil
	}

	result, err := SelectServerFrom(servers, promptFn)
	if err != nil {
		t.Fatalf("SelectServerFrom() error = %v", err)
	}

	if result.Server != "api.example.com" {
		t.Errorf("Server = %v, want api.example.com", result.Server)
	}
	if result.Selected {
		t.Error("Selected should be false for auto-selection")
	}
}

func TestSelectServerFrom_MultipleServers(t *testing.T) {
	servers := []string{"api.example.com", "api2.example.com", "api3.example.com"}

	// Mock prompt function that selects the second server
	promptFn := func(servers []string) (string, error) {
		if len(servers) != 3 {
			t.Errorf("promptFn received %d servers, want 3", len(servers))
		}
		return "api2.example.com", nil
	}

	result, err := SelectServerFrom(servers, promptFn)
	if err != nil {
		t.Fatalf("SelectServerFrom() error = %v", err)
	}

	if result.Server != "api2.example.com" {
		t.Errorf("Server = %v, want api2.example.com", result.Server)
	}
	if !result.Selected {
		t.Error("Selected should be true for user selection")
	}
}

func TestSelectServerFrom_NoServers(t *testing.T) {
	servers := []string{}

	promptFn := func(servers []string) (string, error) {
		t.Error("promptFn should not be called for empty servers")
		return "", nil
	}

	_, err := SelectServerFrom(servers, promptFn)
	if err == nil {
		t.Error("SelectServerFrom() should error for empty servers")
	}
	if err.Error() != "no servers configured. Run 'haloy server add <url> <token>' first" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestSelectServerFrom_PromptError(t *testing.T) {
	servers := []string{"api.example.com", "api2.example.com"}

	// Mock prompt function that returns an error
	promptFn := func(servers []string) (string, error) {
		return "", errors.New("user cancelled")
	}

	_, err := SelectServerFrom(servers, promptFn)
	if err == nil {
		t.Error("SelectServerFrom() should propagate prompt error")
	}
	if err.Error() != "server selection cancelled: user cancelled" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestSelectServerFrom_NilServers(t *testing.T) {
	var servers []string = nil

	promptFn := func(servers []string) (string, error) {
		t.Error("promptFn should not be called for nil servers")
		return "", nil
	}

	_, err := SelectServerFrom(servers, promptFn)
	if err == nil {
		t.Error("SelectServerFrom() should error for nil servers")
	}
}
