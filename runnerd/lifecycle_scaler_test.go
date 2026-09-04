package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/actions/scaleset"
)

func TestLifecycleScalerRestoresCancellationRemovedByListener(t *testing.T) {
	lifecycle, stop := context.WithCancel(context.Background())
	inner := &blockingScaler{started: make(chan struct{})}
	scaler := newLifecycleScaler(lifecycle, inner)
	result := make(chan error, 1)
	go func() {
		_, err := scaler.HandleDesiredRunnerCount(context.WithoutCancel(lifecycle), 1)
		result <- err
	}()
	<-inner.started
	stop()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("HandleDesiredRunnerCount error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HandleDesiredRunnerCount did not stop with the daemon lifecycle")
	}
}

type blockingScaler struct {
	started chan struct{}
}

func (s *blockingScaler) HandleJobStarted(context.Context, *scaleset.JobStarted) error {
	return nil
}

func (s *blockingScaler) HandleJobCompleted(context.Context, *scaleset.JobCompleted) error {
	return nil
}

func (s *blockingScaler) HandleDesiredRunnerCount(ctx context.Context, _ int) (int, error) {
	close(s.started)
	<-ctx.Done()
	return 0, ctx.Err()
}
