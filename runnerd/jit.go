package main

import (
	"context"
	"errors"
	"fmt"

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
