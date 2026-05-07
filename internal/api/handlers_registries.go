package api

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
)

func (s *APIServer) handleRegistriesList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		registries, err := loadServerRegistries()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		entries := make([]apitypes.RegistryEntry, 0, len(registries.Registries))
		for server, auth := range registries.Registries {
			entries = append(entries, apitypes.RegistryEntry{
				Server:   server,
				Username: auth.Username.Value,
			})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Server < entries[j].Server
		})

		encodeJSON(w, http.StatusOK, apitypes.RegistriesResponse{Registries: entries})
	}
}

func (s *APIServer) handleRegistryLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.RegistryLoginRequest
		if err := decodeJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		server := config.NormalizeRegistryServer(req.Server)
		if server == "" {
			http.Error(w, "registry server is required", http.StatusBadRequest)
			return
		}
		if req.Username == "" {
			http.Error(w, "registry username is required", http.StatusBadRequest)
			return
		}
		if req.Password == "" {
			http.Error(w, "registry password is required", http.StatusBadRequest)
			return
		}

		registries, err := loadServerRegistries()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		registries.Registries[server] = config.RegistryAuth{
			Server:   server,
			Username: config.ValueSource{Value: req.Username},
			Password: config.ValueSource{Value: req.Password},
		}
		if err := registries.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := saveServerRegistries(registries); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		encodeJSON(w, http.StatusOK, apitypes.RegistryEntry{
			Server:   server,
			Username: req.Username,
		})
	}
}

func (s *APIServer) handleRegistryLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.RegistryLogoutRequest
		if err := decodeJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		server := config.NormalizeRegistryServer(req.Server)
		if server == "" {
			http.Error(w, "registry server is required", http.StatusBadRequest)
			return
		}

		registries, err := loadServerRegistries()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, exists := registries.Registries[server]; !exists {
			http.Error(w, fmt.Sprintf("registry credentials for %s were not found", server), http.StatusNotFound)
			return
		}
		delete(registries.Registries, server)
		if err := saveServerRegistries(registries); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func loadServerRegistries() (*config.ServerRegistriesConfig, error) {
	path, err := config.ServerRegistriesPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get server registry config path: %w", err)
	}

	registries, err := config.LoadServerRegistries(path)
	if err != nil {
		return nil, err
	}
	if err := registries.Validate(); err != nil {
		return nil, err
	}
	return registries, nil
}

func saveServerRegistries(registries *config.ServerRegistriesConfig) error {
	path, err := config.ServerRegistriesPath()
	if err != nil {
		return fmt.Errorf("failed to get server registry config path: %w", err)
	}
	return config.SaveServerRegistries(registries, path)
}
