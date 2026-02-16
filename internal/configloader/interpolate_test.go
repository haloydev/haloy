package configloader

import (
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/config"
)

func makeEnv(name, value string) config.EnvVar {
	return config.EnvVar{
		Name: name,
		ValueSource: config.ValueSource{
			Value: value,
		},
	}
}

func TestInterpolateEnvVars_NoInterpolation(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("FOO", "bar"),
		makeEnv("BAZ", "qux"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envVars[0].Value != "bar" {
		t.Errorf("expected 'bar', got '%s'", envVars[0].Value)
	}
	if envVars[1].Value != "qux" {
		t.Errorf("expected 'qux', got '%s'", envVars[1].Value)
	}
}

func TestInterpolateEnvVars_SimpleReference(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("PASSWORD", "secret123"),
		makeEnv("DATABASE_URL", "postgresql://user:${PASSWORD}@host:5432/db"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "postgresql://user:secret123@host:5432/db"
	if envVars[1].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars[1].Value)
	}
}

func TestInterpolateEnvVars_ChainedReferences(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("C", "${B}-end"),
		makeEnv("B", "${A}-middle"),
		makeEnv("A", "start"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envVars[2].Value != "start" {
		t.Errorf("expected 'start', got '%s'", envVars[2].Value)
	}
	if envVars[1].Value != "start-middle" {
		t.Errorf("expected 'start-middle', got '%s'", envVars[1].Value)
	}
	if envVars[0].Value != "start-middle-end" {
		t.Errorf("expected 'start-middle-end', got '%s'", envVars[0].Value)
	}
}

func TestInterpolateEnvVars_MultipleReferencesInOneValue(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("SCHEME", "https"),
		makeEnv("HOST", "example.com"),
		makeEnv("PORT", "8443"),
		makeEnv("URL", "${SCHEME}://${HOST}:${PORT}"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "https://example.com:8443"
	if envVars[3].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars[3].Value)
	}
}

func TestInterpolateEnvVars_UndefinedReferenceLeftAsLiteral(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("FOO", "hello-${UNKNOWN}-world"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "hello-${UNKNOWN}-world"
	if envVars[0].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars[0].Value)
	}
}

func TestInterpolateEnvVars_CycleDetection(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("A", "${B}"),
		makeEnv("B", "${A}"),
	}
	err := InterpolateEnvVars(envVars)
	if err == nil {
		t.Fatal("expected error for circular dependency, got nil")
	}
}

func TestInterpolateEnvVars_SelfReference(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("A", "${A}-suffix"),
	}
	err := InterpolateEnvVars(envVars)
	if err == nil {
		t.Fatal("expected error for self-reference, got nil")
	}
}

func TestInterpolateEnvVars_MixedMatchedAndUnmatched(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("DB_PASS", "secret"),
		makeEnv("CONNECTION", "host=${DB_PASS}&opts=${NOT_DEFINED}"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "host=secret&opts=${NOT_DEFINED}"
	if envVars[1].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars[1].Value)
	}
}

func TestInterpolateEnvVars_EmptyList(t *testing.T) {
	if err := InterpolateEnvVars(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := InterpolateEnvVars([]config.EnvVar{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInterpolateEnvVars_OrderIndependence(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("URL", "postgresql://user:${PASSWORD}@host/db"),
		makeEnv("PASSWORD", "secret"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "postgresql://user:secret@host/db"
	if envVars[0].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars[0].Value)
	}
}

func TestInterpolateEnvVars_ShellSyntaxLeftAlone(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("JAVA_OPTS", "-Xmx512m"),
		makeEnv("CMD", "java ${JAVA_OPTS} -jar app.jar"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "java -Xmx512m -jar app.jar"
	if envVars[1].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars[1].Value)
	}
}

func TestInterpolateEnvVars_FromSecretNotInterpolated(t *testing.T) {
	envVars := []config.EnvVar{
		{
			Name: "PASSWORD",
			ValueSource: config.ValueSource{
				Value: "resolved-secret",
			},
		},
		makeEnv("URL", "postgresql://user:${PASSWORD}@host/db"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "postgresql://user:resolved-secret@host/db"
	if envVars[1].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars[1].Value)
	}
}

func TestInterpolateEnvVars_ThreeNodeCycle(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("A", "${B}"),
		makeEnv("B", "${C}"),
		makeEnv("C", "${A}"),
	}
	err := InterpolateEnvVars(envVars)
	if err == nil {
		t.Fatal("expected error for circular dependency, got nil")
	}
}

func TestInterpolateEnvVars_DuplicateNames(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("PASSWORD", "first"),
		makeEnv("PASSWORD", "second"),
		makeEnv("URL", "user:${PASSWORD}@host"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "user:second@host"
	if envVars[2].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars[2].Value)
	}
}

func TestInterpolateEnvVars_ReferenceToEmptyValue(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("EMPTY", ""),
		makeEnv("URL", "prefix-${EMPTY}-suffix"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// EMPTY has Value "", so it's not in nameIndex as a dependency source
	// (the graph-building loop skips ev.Value == ""), but the reference
	// is still resolved at replacement time via nameIndex lookup
	expected := "prefix--suffix"
	if envVars[1].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars[1].Value)
	}
}

func TestInterpolateEnvVars_RepeatedSameReference(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("X", "val"),
		makeEnv("DOUBLED", "${X}/${X}"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "val/val"
	if envVars[1].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars[1].Value)
	}
}

func TestInterpolateEnvVars_AdjacentReferences(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("A", "hello"),
		makeEnv("B", "world"),
		makeEnv("C", "${A}${B}"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "helloworld"
	if envVars[2].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars[2].Value)
	}
}

func TestInterpolateEnvVars_EntireValueIsReference(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("SOURCE", "the-value"),
		makeEnv("COPY", "${SOURCE}"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envVars[1].Value != "the-value" {
		t.Errorf("expected 'the-value', got '%s'", envVars[1].Value)
	}
}

func TestInterpolateEnvVars_NonMatchingDollarPatterns(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("BARE", "$VAR stays"),
		makeEnv("EMPTY_BRACES", "${} stays"),
		makeEnv("NUMERIC_START", "${123BAD} stays"),
		makeEnv("HYPHEN", "${A-B} stays"),
		makeEnv("DOLLAR_ONLY", "just $ here"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envVars[0].Value != "$VAR stays" {
		t.Errorf("bare $VAR was modified: got '%s'", envVars[0].Value)
	}
	if envVars[1].Value != "${} stays" {
		t.Errorf("empty braces was modified: got '%s'", envVars[1].Value)
	}
	if envVars[2].Value != "${123BAD} stays" {
		t.Errorf("numeric start was modified: got '%s'", envVars[2].Value)
	}
	if envVars[3].Value != "${A-B} stays" {
		t.Errorf("hyphen in name was modified: got '%s'", envVars[3].Value)
	}
	if envVars[4].Value != "just $ here" {
		t.Errorf("lone dollar was modified: got '%s'", envVars[4].Value)
	}
}

func TestInterpolateEnvVars_NoDoubleInterpolation(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("INNER", "resolved"),
		makeEnv("OUTER", "literal-${INNER}-text"),
		makeEnv("CONSUMER", "got:${OUTER}"),
	}
	if err := InterpolateEnvVars(envVars); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// OUTER resolves to "literal-resolved-text", CONSUMER gets that as-is
	if envVars[2].Value != "got:literal-resolved-text" {
		t.Errorf("expected 'got:literal-resolved-text', got '%s'", envVars[2].Value)
	}

	// Now test that a resolved value containing ${...} literal text is NOT re-interpolated
	envVars2 := []config.EnvVar{
		makeEnv("TMPL", "use ${RUNTIME_VAR} here"),
		makeEnv("CONFIG", "template=${TMPL}"),
	}
	// RUNTIME_VAR is not defined, so TMPL keeps the literal ${RUNTIME_VAR}
	if err := InterpolateEnvVars(envVars2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "template=use ${RUNTIME_VAR} here"
	if envVars2[1].Value != expected {
		t.Errorf("expected '%s', got '%s'", expected, envVars2[1].Value)
	}
}

func TestInterpolateEnvVars_CycleErrorContainsVarNames(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("ALPHA", "${BETA}"),
		makeEnv("BETA", "${ALPHA}"),
	}
	err := InterpolateEnvVars(envVars)
	if err == nil {
		t.Fatal("expected error for circular dependency, got nil")
	}
	if !strings.Contains(err.Error(), "ALPHA") || !strings.Contains(err.Error(), "BETA") {
		t.Errorf("error should mention involved var names, got: %s", err.Error())
	}
}

func TestInterpolateEnvVars_SelfReferenceErrorContainsVarName(t *testing.T) {
	envVars := []config.EnvVar{
		makeEnv("MYSELF", "${MYSELF}-more"),
	}
	err := InterpolateEnvVars(envVars)
	if err == nil {
		t.Fatal("expected error for self-reference, got nil")
	}
	if !strings.Contains(err.Error(), "MYSELF") {
		t.Errorf("error should mention the var name, got: %s", err.Error())
	}
}
