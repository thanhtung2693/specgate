package integrations

import (
	"context"
	"errors"
	"testing"
)

func TestBindIntegrationWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("binds owner to unscoped callback", func(t *testing.T) {
		t.Parallel()

		ctx, err := bindIntegrationWorkspace(context.Background(), &Integration{WorkspaceID: "ws-owner"})
		if err != nil {
			t.Fatal(err)
		}
		if got := WorkspaceID(ctx); got != "ws-owner" {
			t.Fatalf("workspace = %q, want ws-owner", got)
		}
	})

	t.Run("accepts matching selected workspace", func(t *testing.T) {
		t.Parallel()

		ctx, err := bindIntegrationWorkspace(
			WithWorkspace(context.Background(), "ws-owner"),
			&Integration{WorkspaceID: "ws-owner"},
		)
		if err != nil {
			t.Fatal(err)
		}
		if got := WorkspaceID(ctx); got != "ws-owner" {
			t.Fatalf("workspace = %q, want ws-owner", got)
		}
	})

	t.Run("rejects missing owner", func(t *testing.T) {
		t.Parallel()

		_, err := bindIntegrationWorkspace(context.Background(), &Integration{})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
	})

	t.Run("hides conflicting owner", func(t *testing.T) {
		t.Parallel()

		_, err := bindIntegrationWorkspace(
			WithWorkspace(context.Background(), "ws-selected"),
			&Integration{WorkspaceID: "ws-owner"},
		)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})
}
