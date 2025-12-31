package config

import (
	"reflect"
	"testing"
)

func TestCheckUnknownFields(t *testing.T) {
	deployConfigType := reflect.TypeOf(DeployConfig{})
	tests := []struct {
		name    string
		keys    []string
		wantErr bool
	}{
		{"valid simple", []string{"name"}, false},
		{"valid nested", []string{"env", "env.value", "image.registry", "image.tag"}, false},
		{"invalid simple", []string{"notHere"}, true},
		{"invalid nested", []string{"env", "env.unknown", "env.unknown.childunknown"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckUnknownFields(deployConfigType, tt.keys, "json"); (err == nil) == tt.wantErr {
				t.Errorf("TestCheckUnknownFields() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
