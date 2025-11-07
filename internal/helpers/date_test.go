package helpers

import (
	"testing"
	"time"

	"github.com/oklog/ulid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTimestampFromDeploymentID(t *testing.T) {
	tests := []struct {
		name         string
		deploymentID string
		wantErr      bool
	}{
		{
			name:         "empty_string",
			deploymentID: "",
			wantErr:      true,
		},
		{
			name:         "too_short",
			deploymentID: "123",
			wantErr:      true,
		},
		{
			name:         "contains_invalid_char_exclamation",
			deploymentID: "01H7VXPQZK9XYZ123456!@#",
			wantErr:      true,
		},
		{
			name:         "contains_invalid_char_lowercase_o",
			deploymentID: "01H7VXPQZKoXYZ12340AB",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetTimestampFromDeploymentID(tt.deploymentID)

			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, result.IsZero())
			} else {
				assert.NoError(t, err)
				assert.False(t, result.IsZero())
			}
		})
	}
}

func TestGetTimestampFromDeploymentID_ValidULID(t *testing.T) {
	// Create a real ULID for testing
	now := time.Now()
	entropy := make([]byte, 10)
	// Fill with zeros for predictable test
	for i := range entropy {
		entropy[i] = 0
	}

	ulidValue, err := ulid.New(ulid.Timestamp(now), &simpleEntropy{data: entropy})
	require.NoError(t, err)

	result, err := GetTimestampFromDeploymentID(ulidValue.String())
	assert.NoError(t, err)
	assert.False(t, result.IsZero())

	// Should be close to the original time (within 1 second)
	diff := result.Sub(now)
	assert.True(t, diff >= -time.Second && diff <= time.Second)
}

func TestFormatDateString_BasicCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "empty_string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid_format",
			input:   "not-a-date",
			wantErr: true,
		},
		{
			name:    "14_char_format",
			input:   "20250812150000",
			wantErr: false,
		},
		{
			name:    "rfc3339_format",
			input:   "2025-08-12T15:00:00Z",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FormatDateString(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, result)
				// Should contain time indicator
				hasIndicator := assert.Contains(t, result, "ago") || assert.Contains(t, result, "from now")
				assert.True(t, hasIndicator, "Expected 'ago' or 'from now' in: %s", result)
			}
		})
	}
}

func TestFormatDateStringWithLocation_BasicCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid_14_char",
			input:   "20250812150000",
			wantErr: false,
		},
		{
			name:    "invalid_input",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FormatDateStringWithLocation(tt.input, time.UTC)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, result)
			}
		})
	}
}

func TestFormatTime_Basic(t *testing.T) {
	// Test with a time in the past
	pastTime := time.Now().Add(-1 * time.Hour)
	result := FormatTime(pastTime)

	assert.NotEmpty(t, result)
	assert.Contains(t, result, "ago")
}

func TestFormatTimeWithLocation_Basic(t *testing.T) {
	// Test with a time in the past
	pastTime := time.Now().Add(-1 * time.Hour)
	result := FormatTimeWithLocation(pastTime, time.UTC)

	assert.NotEmpty(t, result)
	assert.Contains(t, result, "ago")
}

// Simple entropy source for testing
type simpleEntropy struct {
	data []byte
	pos  int
}

func (e *simpleEntropy) Read(p []byte) (n int, err error) {
	for i := range p {
		if e.pos >= len(e.data) {
			e.pos = 0
		}
		p[i] = e.data[e.pos]
		e.pos++
	}
	return len(p), nil
}
