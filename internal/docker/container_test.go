package docker

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/client"
)

func TestStopDisappearingContainer(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stopStatus int
		want       []string
	}{
		{"gone before stop", 404, []string{"stop"}},
		{"gone before kill", 500, []string{"stop", "kill"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				action := path.Base(r.URL.Path)
				calls = append(calls, action)
				if action == "stop" {
					w.WriteHeader(tc.stopStatus)
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			cli, err := client.NewClientWithOpts(client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")), client.WithVersion("1.47"))
			if err != nil {
				t.Fatal(err)
			}
			defer cli.Close()
			err = stopSingleContainer(t.Context(), cli, slog.Default(), "test-container")
			server.Close()
			if err != nil || !slices.Equal(calls, tc.want) {
				t.Fatalf("calls %v, error %v", calls, err)
			}
		})
	}
}
