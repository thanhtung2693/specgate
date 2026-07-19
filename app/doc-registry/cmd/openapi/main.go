package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/specgate/doc-registry/internal/api"
	"github.com/specgate/doc-registry/internal/config"
)

func main() {
	handler := (&api.Router{
		Handlers: &api.Handlers{},
		Config:   &config.Config{OpenAPI: config.OpenAPIConfig{Enabled: true}},
	}).Build()
	request := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		fmt.Fprintf(os.Stderr, "generate OpenAPI: status %d: %s\n", response.Code, response.Body.String())
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(response.Body.Bytes()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
