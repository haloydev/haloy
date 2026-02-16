package configloader

import (
	"fmt"
	"regexp"

	"github.com/haloydev/haloy/internal/config"
)

var envVarRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func InterpolateEnvVars(envVars []config.EnvVar) error {
	if len(envVars) == 0 {
		return nil
	}

	nameIndex := make(map[string]int, len(envVars))
	for i, ev := range envVars {
		nameIndex[ev.Name] = i
	}

	inDegree := make(map[int]int, len(envVars))
	dependents := make(map[int][]int, len(envVars))

	for i, ev := range envVars {
		if ev.Value == "" {
			continue
		}
		matches := envVarRefPattern.FindAllStringSubmatch(ev.Value, -1)
		seen := make(map[int]bool)
		for _, match := range matches {
			refName := match[1]
			depIdx, defined := nameIndex[refName]
			if !defined {
				continue
			}
			if depIdx == i {
				return fmt.Errorf("env var '%s' references itself", ev.Name)
			}
			if seen[depIdx] {
				continue
			}
			seen[depIdx] = true
			inDegree[i]++
			dependents[depIdx] = append(dependents[depIdx], i)
		}
	}

	var queue []int
	for i := range envVars {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}

	resolved := 0
	order := make([]int, 0, len(envVars))
	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		order = append(order, idx)
		resolved++
		for _, dep := range dependents[idx] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if resolved != len(envVars) {
		var cycleVars []string
		for i, ev := range envVars {
			if inDegree[i] > 0 {
				cycleVars = append(cycleVars, ev.Name)
			}
		}
		return fmt.Errorf("circular dependency detected among env vars: %v", cycleVars)
	}

	for _, i := range order {
		if envVars[i].Value == "" {
			continue
		}
		envVars[i].Value = envVarRefPattern.ReplaceAllStringFunc(envVars[i].Value, func(match string) string {
			sub := envVarRefPattern.FindStringSubmatch(match)
			refName := sub[1]
			depIdx, defined := nameIndex[refName]
			if !defined {
				return match
			}
			return envVars[depIdx].Value
		})
	}

	return nil
}
