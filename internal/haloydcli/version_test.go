package haloydcli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/haloydev/haloy/internal/proxywire"
)

func TestVersionJSON(t *testing.T) {
	cmd := versionCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	var metadata struct {
		RequiredProxyGeneration    int `json:"required_proxy_generation"`
		RequiredProxySchemaVersion int `json:"required_proxy_schema_version"`
	}
	if err := json.Unmarshal(output.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.RequiredProxyGeneration != proxywire.ProxyGeneration {
		t.Fatalf("required generation = %d, want %d", metadata.RequiredProxyGeneration, proxywire.ProxyGeneration)
	}
	if metadata.RequiredProxySchemaVersion != proxywire.SchemaVersion {
		t.Fatalf("required schema = %d, want %d", metadata.RequiredProxySchemaVersion, proxywire.SchemaVersion)
	}
}
