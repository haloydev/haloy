package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func encodeJSON(w http.ResponseWriter, status int, data any) error {
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(status)

	return json.NewEncoder(w).Encode(data)
}

// decodeJSON reads a JSON-encoded value from an io.Reader and decodes it
func decodeJSON(r io.Reader, v any) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return errors.New("failed to read request body")
	}

	// Create a new decoder from the body we just read
	dec := json.NewDecoder(strings.NewReader(string(body)))

	// Disallow unknown fields in the JSON. If the client sends a field
	// that doesn't exist in our struct, this will cause an error.
	dec.DisallowUnknownFields()

	err = dec.Decode(v)
	if err != nil {
		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError

		switch {
		case errors.As(err, &syntaxError):
			return errors.New("request body contains badly-formed JSON")

		case errors.As(err, &unmarshalTypeError):
			return fmt.Errorf("request body contains an invalid value for the '%s' field", unmarshalTypeError.Field)

		case errors.Is(err, io.EOF):
			return errors.New("request body must not be empty")

		default:
			return err
		}
	}

	return nil
}
