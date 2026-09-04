package main

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/actions/scaleset"
)

func TestScaleSetJITSourcePreservesRunnerIDAndRemovalIsIdempotent(t *testing.T) {
	client := &fakeJITClient{
		expectedName: "aeons-oldtimer-0123456789ab",
		removeErr:    fmt.Errorf("already gone: %w", scaleset.RunnerNotFoundError),
	}
	source := ScaleSetJITSource{Client: client, ScaleSetID: 42}

	got, err := source.Generate(context.Background(), "aeons-oldtimer-0123456789ab")
	if err != nil {
		t.Fatalf("Generate returned an error: %v", err)
	}
	want := JITRunner{Config: "encoded-jit", ServerID: 123}
	if got != want {
		t.Fatalf("JIT runner %#v, want %#v", got, want)
	}
	if err := source.Remove(context.Background(), 123); err != nil {
		t.Fatalf("Remove returned an error for an already-removed runner: %v", err)
	}
}

func TestRetryingJITClientPreservesMessageSessionAcrossNetworkFailures(t *testing.T) {
	networkErr := &net.DNSError{Err: "network is unreachable", Name: "api.github.com", IsTemporary: true}
	inner := &fakeJITClient{
		expectedName:   "runner",
		generateErrors: []error{networkErr},
		removeErrors:   []error{networkErr},
	}
	waits := 0
	client := newRetryingJITClient(inner, time.Second, func(context.Context, time.Duration) error {
		waits++
		return nil
	})
	ctx := context.Background()

	if _, err := client.GenerateJitRunnerConfig(ctx, &scaleset.RunnerScaleSetJitRunnerSetting{Name: "runner"}, 42); err != nil {
		t.Fatalf("GenerateJitRunnerConfig returned an error: %v", err)
	}
	if err := client.RemoveRunner(ctx, 123); err != nil {
		t.Fatalf("RemoveRunner returned an error: %v", err)
	}
	if inner.generateCalls != 2 || inner.removeCalls != 2 || waits != 2 {
		t.Fatalf("calls generate=%d remove=%d waits=%d, want 2/2/2", inner.generateCalls, inner.removeCalls, waits)
	}
}

type fakeJITClient struct {
	expectedName   string
	generateErrors []error
	removeErrors   []error
	removeErr      error
	generateCalls  int
	removeCalls    int
}

func (c *fakeJITClient) GenerateJitRunnerConfig(_ context.Context, setting *scaleset.RunnerScaleSetJitRunnerSetting, scaleSetID int) (*scaleset.RunnerScaleSetJitRunnerConfig, error) {
	c.generateCalls++
	if len(c.generateErrors) > 0 {
		err := c.generateErrors[0]
		c.generateErrors = c.generateErrors[1:]
		return nil, err
	}
	if setting.Name != c.expectedName || scaleSetID != 42 {
		return nil, fmt.Errorf("unexpected JIT request")
	}
	return &scaleset.RunnerScaleSetJitRunnerConfig{
		Runner:           &scaleset.RunnerReference{ID: 123},
		EncodedJITConfig: "encoded-jit",
	}, nil
}

func (c *fakeJITClient) RemoveRunner(_ context.Context, _ int64) error {
	c.removeCalls++
	if len(c.removeErrors) > 0 {
		err := c.removeErrors[0]
		c.removeErrors = c.removeErrors[1:]
		return err
	}
	return c.removeErr
}
