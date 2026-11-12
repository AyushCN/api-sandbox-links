package api

import (
	"context"
	"testing"

	"github.com/api-sandbox-links/backend/internal/db"
)

func TestTouchSandboxGuards(t *testing.T) {
	ctx := context.Background()

	// 1. Nil server should not panic
	var nilServer *Server
	nilServer.touchSandbox(ctx, &db.Sandbox{ID: "test-sb"})

	// 2. Server with nil cache should not panic
	srv := &Server{cache: nil}
	srv.touchSandbox(ctx, nil)
	srv.touchSandbox(ctx, &db.Sandbox{ID: ""})
	srv.touchSandbox(ctx, &db.Sandbox{ID: "sb-123", IdleTimeoutSeconds: 600})
}

