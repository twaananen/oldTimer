package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"syscall"
	"time"

	"github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
)

type retryingSessionClient struct {
	lifecycle  context.Context
	inner      listener.Client
	retryDelay time.Duration
	wait       func(context.Context, time.Duration) error
}

func newRetryingSessionClient(
	lifecycle context.Context,
	inner listener.Client,
	retryDelay time.Duration,
	wait func(context.Context, time.Duration) error,
) *retryingSessionClient {
	return &retryingSessionClient{lifecycle: lifecycle, inner: inner, retryDelay: retryDelay, wait: wait}
}

func (c *retryingSessionClient) GetMessage(ctx context.Context, lastMessageID, maxCapacity int) (*scaleset.RunnerScaleSetMessage, error) {
	return retrySessionCall(ctx, c, "get message", func(callCtx context.Context) (*scaleset.RunnerScaleSetMessage, error) {
		return c.inner.GetMessage(callCtx, lastMessageID, maxCapacity)
	})
}

func (c *retryingSessionClient) DeleteMessage(ctx context.Context, messageID int) error {
	_, err := retrySessionCall(ctx, c, "delete message", func(callCtx context.Context) (struct{}, error) {
		return struct{}{}, c.inner.DeleteMessage(callCtx, messageID)
	})
	return err
}

func (c *retryingSessionClient) AcquireJobs(ctx context.Context, requestIDs []int64) ([]int64, error) {
	return retrySessionCall(ctx, c, "acquire jobs", func(callCtx context.Context) ([]int64, error) {
		return c.inner.AcquireJobs(callCtx, requestIDs)
	})
}

func (c *retryingSessionClient) Session() scaleset.RunnerScaleSetSession {
	return c.inner.Session()
}

func retrySessionCall[T any](ctx context.Context, client *retryingSessionClient, operation string, call func(context.Context) (T, error)) (T, error) {
	for {
		callCtx, cancelCall := linkedContext(ctx, client.lifecycle)
		result, err := call(callCtx)
		cancelCall()
		if err == nil {
			return result, err
		}
		if client.lifecycle.Err() != nil {
			return result, client.lifecycle.Err()
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if !isTransientNetworkError(err) {
			return result, err
		}
		slog.Warn("runner message session interrupted; retrying", "operation", operation, "error", err, "retry_in", client.retryDelay)
		waitCtx, cancelWait := linkedContext(ctx, client.lifecycle)
		err = client.wait(waitCtx, client.retryDelay)
		cancelWait()
		if err != nil {
			var zero T
			return zero, err
		}
	}
}

func linkedContext(request, lifecycle context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(request)
	stopLifecycle := context.AfterFunc(lifecycle, cancel)
	return ctx, func() {
		stopLifecycle()
		cancel()
	}
}

func isTransientNetworkError(err error) bool {
	var networkErr net.Error
	if errors.As(err, &networkErr) && (networkErr.Timeout() || networkErr.Temporary()) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ENETDOWN) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH)
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var _ listener.Client = (*retryingSessionClient)(nil)
