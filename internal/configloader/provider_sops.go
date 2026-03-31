package configloader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/getsops/sops/v3/decrypt"
	"github.com/haloydev/haloy/internal/config"
	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

var (
	sopsReadFileFn    = os.ReadFile
	sopsDecryptDataFn = decrypt.Data
)

func fetchFromSOPS(ctx context.Context, cfg config.SOPSSourceConfig) (map[string]string, error) {
	if strings.TrimSpace(cfg.File) == "" {
		return nil, formatSOPSError("validate", "", fmt.Errorf("sops source requires 'file' to be set"))
	}

	resolvedPath := resolveSOPSPath(cfg.File, "")

	format, err := normalizeSOPSFormat(cfg.Format, cfg.File)
	if err != nil {
		return nil, formatSOPSError("validate", cfg.File, err)
	}

	encryptedData, err := sopsReadFileFn(resolvedPath)
	if err != nil {
		return nil, formatSOPSError("read", cfg.File, err)
	}

	plaintext, err := sopsDecryptDataFn(encryptedData, format)
	if err != nil {
		return nil, formatSOPSError("decrypt", cfg.File, err)
	}

	values, err := parseSOPSPlaintext(format, plaintext)
	if err != nil {
		return nil, formatSOPSError("parse", cfg.File, err)
	}

	return values, nil
}

func formatSOPSError(stage, path string, cause error) error {
	if strings.TrimSpace(path) != "" {
		return fmt.Errorf("provider=sops stage=%s path=%s: %w", stage, path, cause)
	}
	return fmt.Errorf("provider=sops stage=%s: %w", stage, cause)
}

func resolveSOPSPath(filePath, baseDir string) string {
	if filepath.IsAbs(filePath) {
		return filepath.Clean(filePath)
	}

	if baseDir == "" {
		return filepath.Clean(filePath)
	}

	return filepath.Clean(filepath.Join(baseDir, filePath))
}

func normalizeSOPSFormat(format, filePath string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(format))
	if normalized != "" {
		switch normalized {
		case "yaml", "json", "dotenv":
			return normalized, nil
		case "binary":
			return "", fmt.Errorf("format 'binary' is not supported for key-based secret resolution")
		default:
			return "", fmt.Errorf("unsupported sops format '%s' (supported: yaml, json, dotenv)", normalized)
		}
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".yaml", ".yml":
		return "yaml", nil
	case ".json":
		return "json", nil
	case ".env", ".dotenv":
		return "dotenv", nil
	default:
		return "", fmt.Errorf("could not infer sops format from file extension '%s'; set 'format' to one of: yaml, json, dotenv", ext)
	}
}

func parseSOPSPlaintext(format string, plaintext []byte) (map[string]string, error) {
	switch format {
	case "dotenv":
		return godotenv.Unmarshal(string(plaintext))
	case "yaml":
		var data any
		if err := yaml.Unmarshal(plaintext, &data); err != nil {
			return nil, err
		}
		return flattenStructuredData(data)
	case "json":
		var data any
		decoder := json.NewDecoder(strings.NewReader(string(plaintext)))
		decoder.UseNumber()
		if err := decoder.Decode(&data); err != nil {
			return nil, err
		}
		return flattenStructuredData(data)
	default:
		return nil, fmt.Errorf("unsupported sops format '%s'", format)
	}
}

func flattenStructuredData(data any) (map[string]string, error) {
	out := make(map[string]string)
	if err := flattenValue(data, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

func flattenValue(value any, path []string, out map[string]string) error {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if strings.Contains(key, ".") {
				return fmt.Errorf("object key '%s' contains '.' which is not supported", key)
			}
			if err := flattenValue(typed[key], append(path, key), out); err != nil {
				return err
			}
		}
		return nil
	case map[any]any:
		converted := make(map[string]any, len(typed))
		for key, val := range typed {
			keyString, ok := key.(string)
			if !ok {
				return fmt.Errorf("object key '%v' must be a string", key)
			}
			converted[keyString] = val
		}
		return flattenValue(converted, path, out)
	case []any:
		for i, item := range typed {
			if err := flattenValue(item, append(path, strconv.Itoa(i)), out); err != nil {
				return err
			}
		}
		return nil
	default:
		if len(path) == 0 {
			return fmt.Errorf("top-level scalar values are not supported; expected object or array")
		}
		key := strings.Join(path, ".")
		if _, exists := out[key]; exists {
			return fmt.Errorf("flattened key collision at '%s'", key)
		}
		scalar, err := scalarToString(typed)
		if err != nil {
			return fmt.Errorf("key '%s': %w", key, err)
		}
		out[key] = scalar
		return nil
	}
}

func scalarToString(value any) (string, error) {
	if value == nil {
		return "", nil
	}

	if number, ok := value.(json.Number); ok {
		if i, err := number.Int64(); err == nil {
			return strconv.FormatInt(i, 10), nil
		}
		f, err := number.Float64()
		if err != nil {
			return "", fmt.Errorf("unsupported number value '%s'", number)
		}
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'f', -1, 32), nil
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("unsupported scalar type %T", value)
	}
}
