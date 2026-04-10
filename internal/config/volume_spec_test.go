package config

import (
	"strings"
	"testing"
)

func TestParseVolumeSpec(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		wantSource     string
		wantTarget     string
		wantOptions    string
		wantSourceType VolumeSourceType
		wantErr        string
	}{
		{
			name:           "named volume with mode",
			raw:            "app-data:/pb/pb_data:ro",
			wantSource:     "app-data",
			wantTarget:     "/pb/pb_data",
			wantOptions:    "ro",
			wantSourceType: VolumeSourceTypeNamed,
		},
		{
			name:           "linux bind mount",
			raw:            "/srv/app-data:/pb/pb_data",
			wantSource:     "/srv/app-data",
			wantTarget:     "/pb/pb_data",
			wantSourceType: VolumeSourceTypeBind,
		},
		{
			name:    "windows bind mount rejected",
			raw:     `C:\data:/pb/pb_data`,
			wantErr: "must use a Linux absolute path",
		},
		{
			name:    "relative bind mount rejected",
			raw:     "./data:/pb/pb_data",
			wantErr: "must be absolute when using filesystem bind mounts",
		},
		{
			name:    "relative container path rejected",
			raw:     "app-data:pb/pb_data",
			wantErr: "is not an absolute path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVolumeSpec(tt.raw)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseVolumeSpec(%q) expected error containing %q, got nil", tt.raw, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseVolumeSpec(%q) error = %q, want substring %q", tt.raw, err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseVolumeSpec(%q) unexpected error: %v", tt.raw, err)
			}

			if got.Source != tt.wantSource {
				t.Fatalf("ParseVolumeSpec(%q) source = %q, want %q", tt.raw, got.Source, tt.wantSource)
			}
			if got.Target != tt.wantTarget {
				t.Fatalf("ParseVolumeSpec(%q) target = %q, want %q", tt.raw, got.Target, tt.wantTarget)
			}
			if got.Options != tt.wantOptions {
				t.Fatalf("ParseVolumeSpec(%q) options = %q, want %q", tt.raw, got.Options, tt.wantOptions)
			}
			if got.SourceType != tt.wantSourceType {
				t.Fatalf("ParseVolumeSpec(%q) sourceType = %q, want %q", tt.raw, got.SourceType, tt.wantSourceType)
			}
		})
	}
}
