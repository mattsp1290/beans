package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestRunStopsWhenContextIsCanceled(t *testing.T) {
	app := New(Config{LogOutput: bytes.NewBuffer(nil)})
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- Run(ctx, app, addr)
	}()

	waitForListen(t, addr)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func TestRunReturnsListenError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	app := New(Config{LogOutput: bytes.NewBuffer(nil)})
	err = Run(context.Background(), app, listener.Addr().String())
	if err == nil {
		t.Fatal("Run error = nil, want listen error")
	}
}

func TestRunShutdownTimeoutBoundsLongRequest(t *testing.T) {
	app := New(Config{LogOutput: bytes.NewBuffer(nil)})
	block := make(chan struct{})
	app.Get("/block", func(c fiber.Ctx) error {
		<-block
		return c.SendString("done")
	})
	defer close(block)

	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, app, addr, RunConfig{ShutdownTimeout: 20 * time.Millisecond})
	}()

	waitForListen(t, addr)
	requestDone := make(chan struct{})
	go func() {
		resp, err := http.Get("http://" + addr + "/block")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		close(requestDone)
	}()
	waitForActiveRequest(t, requestDone)

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after shutdown timeout")
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

func waitForActiveRequest(t *testing.T, done <-chan struct{}) {
	t.Helper()

	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
		t.Fatal("request completed before shutdown test canceled it")
	case <-timer.C:
	}
}

func waitForListen(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("listener did not open")
	}
	t.Fatalf("server did not listen on %s: %v", addr, lastErr)
}
