package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluateFindingsAllowsKnownDockerFindingsWithoutFixedVersion(t *testing.T) {
	input := strings.NewReader(`
{"finding":{"osv":"GO-2026-4883","trace":[{"module":"github.com/docker/docker","version":"v28.0.4+incompatible","package":"github.com/docker/docker/client","function":"ImagePull"},{"module":"github.com/haloydev/haloy","package":"github.com/haloydev/haloy/internal/docker","function":"EnsureImageUpToDate"}]}}
{"finding":{"osv":"GO-2026-4887","trace":[{"module":"github.com/docker/docker","version":"v28.0.4+incompatible","package":"github.com/docker/docker/client","function":"ImagePush"},{"module":"github.com/haloydev/haloy","package":"github.com/haloydev/haloy/internal/docker","function":"PushImage"}]}}
`)

	unexpected, allowed, err := evaluateFindings(input, allowedFindings)

	require.NoError(t, err)
	require.Empty(t, unexpected)
	require.Equal(t, []string{"GO-2026-4883", "GO-2026-4887"}, allowed)
}

func TestEvaluateFindingsFailsAllowedIDWhenFixedVersionExists(t *testing.T) {
	input := strings.NewReader(`
{"finding":{"osv":"GO-2026-4883","fixed_version":"v29.0.0","trace":[{"module":"github.com/docker/docker","version":"v28.0.4+incompatible","package":"github.com/docker/docker/client","function":"ImagePull"}]}}
`)

	unexpected, allowed, err := evaluateFindings(input, allowedFindings)

	require.NoError(t, err)
	require.Equal(t, []string{"GO-2026-4883"}, unexpected)
	require.Empty(t, allowed)
}

func TestEvaluateFindingsFailsUnallowlistedActionableFindings(t *testing.T) {
	input := strings.NewReader(`
{"finding":{"osv":"GO-2099-0001","fixed_version":"v1.2.3","trace":[{"module":"example.com/vulnerable","version":"v1.0.0","package":"example.com/vulnerable","function":"DoThing"},{"module":"github.com/haloydev/haloy","package":"github.com/haloydev/haloy/internal/example","function":"CallThing"}]}}
`)

	unexpected, allowed, err := evaluateFindings(input, allowedFindings)

	require.NoError(t, err)
	require.Equal(t, []string{"GO-2099-0001"}, unexpected)
	require.Empty(t, allowed)
}

func TestEvaluateFindingsIgnoresModuleOnlyFindings(t *testing.T) {
	input := strings.NewReader(`
{"finding":{"osv":"GO-2099-0001","fixed_version":"v1.2.3","trace":[{"module":"example.com/vulnerable","version":"v1.0.0"}]}}
`)

	unexpected, allowed, err := evaluateFindings(input, allowedFindings)

	require.NoError(t, err)
	require.Empty(t, unexpected)
	require.Empty(t, allowed)
}
