package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	container, err := testcontainers.Run(
		ctx,
		"nats:2.10-alpine",
		testcontainers.WithCmd("-js"),
		testcontainers.WithExposedPorts("4222/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("Server is ready")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate NATS: %v", err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "4222/tcp")
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := runAdmission(ctx, fmt.Sprintf("nats://%s:%s", host, port.Port()))
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("delivery attempts = %d, want 2", attempts)
	}
}
