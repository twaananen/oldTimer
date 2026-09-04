package main

import (
	"context"

	"github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
)

type lifecycleScaler struct {
	lifecycle context.Context
	inner     listener.Scaler
}

func newLifecycleScaler(lifecycle context.Context, inner listener.Scaler) *lifecycleScaler {
	return &lifecycleScaler{lifecycle: lifecycle, inner: inner}
}

func (s *lifecycleScaler) HandleJobStarted(ctx context.Context, job *scaleset.JobStarted) error {
	callCtx, cancel := linkedContext(ctx, s.lifecycle)
	defer cancel()
	return s.inner.HandleJobStarted(callCtx, job)
}

func (s *lifecycleScaler) HandleJobCompleted(ctx context.Context, job *scaleset.JobCompleted) error {
	callCtx, cancel := linkedContext(ctx, s.lifecycle)
	defer cancel()
	return s.inner.HandleJobCompleted(callCtx, job)
}

func (s *lifecycleScaler) HandleDesiredRunnerCount(ctx context.Context, count int) (int, error) {
	callCtx, cancel := linkedContext(ctx, s.lifecycle)
	defer cancel()
	return s.inner.HandleDesiredRunnerCount(callCtx, count)
}

var _ listener.Scaler = (*lifecycleScaler)(nil)
