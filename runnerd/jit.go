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
	GetRunnerByName(context.Context, string) (*scaleset.RunnerReference, error)
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
	jit, err := c.inner.GenerateJitRunnerConfig(ctx, setting, scaleSetID)
	if err == nil || !isTransientNetworkError(err) {
		return jit, err
	}

	// Creation is not idempotent: a lost response may hide a registration that
	// GitHub already accepted. Reconcile by the unique runner name instead of
	// submitting the creation request a second time.
	for observation := 0; observation < 2; observation++ {
		runner, reconcileErr := retryNetworkCall(ctx, networkRetry{lifecycle: ctx, retryDelay: c.retryDelay, wait: c.wait}, "find ambiguous JIT runner", func(callCtx context.Context) (*scaleset.RunnerReference, error) {
			return c.inner.GetRunnerByName(callCtx, setting.Name)
		})
		if reconcileErr != nil {
			return nil, errors.Join(err, fmt.Errorf("reconcile ambiguous JIT runner %q: %w", setting.Name, reconcileErr))
		}
		if runner != nil {
			if removeErr := c.RemoveRunner(ctx, int64(runner.ID)); removeErr != nil {
				return nil, errors.Join(err, fmt.Errorf("remove ambiguous JIT runner %q: %w", setting.Name, removeErr))
			}
			return nil, fmt.Errorf("reconciled ambiguous JIT runner %q: %w", setting.Name, err)
		}
		if observation == 0 {
			if waitErr := c.wait(ctx, c.retryDelay); waitErr != nil {
				return nil, errors.Join(err, waitErr)
			}
		}
	}
	return nil, err
}

func (c *retryingJITClient) GetRunnerByName(ctx context.Context, name string) (*scaleset.RunnerReference, error) {
	return retryNetworkCall(ctx, networkRetry{lifecycle: ctx, retryDelay: c.retryDelay, wait: c.wait}, "get JIT runner by name", func(callCtx context.Context) (*scaleset.RunnerReference, error) {
		return c.inner.GetRunnerByName(callCtx, name)
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
