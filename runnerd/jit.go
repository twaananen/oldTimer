package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/actions/scaleset"
)

type JITClient interface {
	GenerateJitRunnerConfig(context.Context, *scaleset.RunnerScaleSetJitRunnerSetting, int) (*scaleset.RunnerScaleSetJitRunnerConfig, error)
	RemoveRunner(context.Context, int64) error
}

type ScaleSetJITSource struct {
	Client     JITClient
	ScaleSetID int
}

type retryingJITClient struct {
	inner      JITClient
	retryDelay time.Duration
	wait       func(context.Context, time.Duration) error
}

func newRetryingJITClient(
	inner JITClient,
	retryDelay time.Duration,
	wait func(context.Context, time.Duration) error,
) *retryingJITClient {
	return &retryingJITClient{inner: inner, retryDelay: retryDelay, wait: wait}
}

func (c *retryingJITClient) GenerateJitRunnerConfig(ctx context.Context, setting *scaleset.RunnerScaleSetJitRunnerSetting, scaleSetID int) (*scaleset.RunnerScaleSetJitRunnerConfig, error) {
	return retryNetworkCall(ctx, networkRetry{lifecycle: ctx, retryDelay: c.retryDelay, wait: c.wait}, "generate JIT runner config", func(callCtx context.Context) (*scaleset.RunnerScaleSetJitRunnerConfig, error) {
		return c.inner.GenerateJitRunnerConfig(callCtx, setting, scaleSetID)
	})
}

func (c *retryingJITClient) RemoveRunner(ctx context.Context, serverID int64) error {
	_, err := retryNetworkCall(ctx, networkRetry{lifecycle: ctx, retryDelay: c.retryDelay, wait: c.wait}, "remove JIT runner", func(callCtx context.Context) (struct{}, error) {
		return struct{}{}, c.inner.RemoveRunner(callCtx, serverID)
	})
	return err
}

var _ JITClient = (*retryingJITClient)(nil)

func (s ScaleSetJITSource) Generate(ctx context.Context, name string) (JITRunner, error) {
	jit, err := s.Client.GenerateJitRunnerConfig(ctx, &scaleset.RunnerScaleSetJitRunnerSetting{Name: name}, s.ScaleSetID)
	if err != nil {
		return JITRunner{}, err
	}
	if jit.Runner == nil || jit.Runner.ID == 0 || jit.EncodedJITConfig == "" {
		return JITRunner{}, errors.New("GitHub returned an incomplete JIT runner configuration")
	}
	return JITRunner{Config: jit.EncodedJITConfig, ServerID: int64(jit.Runner.ID)}, nil
}

func (s ScaleSetJITSource) Remove(ctx context.Context, serverID int64) error {
	err := s.Client.RemoveRunner(ctx, serverID)
	if err == nil || errors.Is(err, scaleset.RunnerNotFoundError) {
		return nil
	}
	return fmt.Errorf("remove GitHub runner %d: %w", serverID, err)
}
