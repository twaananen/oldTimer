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
	inner listener.Client
	retry networkRetry
}

type networkRetry struct {
	lifecycle  context.Context
	retryDelay time.Duration
	wait       func(context.Context, time.Duration) error
}

func newRetryingSessionClient(
	lifecycle context.Context,
	inner listener.Client,
	retryDelay time.Duration,
	wait func(context.Context, time.Duration) error,
) *retryingSessionClient {
	return &retryingSessionClient{
		inner: inner,
		retry: networkRetry{lifecycle: lifecycle, retryDelay: retryDelay, wait: wait},
	}
}

func (c *retryingSessionClient) GetMessage(ctx context.Context, lastMessageID, maxCapacity int) (*scaleset.RunnerScaleSetMessage, error) {
	return retryNetworkCall(ctx, c.retry, "get message", func(callCtx context.Context) (*scaleset.RunnerScaleSetMessage, error) {
		return c.inner.GetMessage(callCtx, lastMessageID, maxCapacity)
	})
}

func (c *retryingSessionClient) DeleteMessage(ctx context.Context, messageID int) error {
	_, err := retryNetworkCall(ctx, c.retry, "delete message", func(callCtx context.Context) (struct{}, error) {
		return struct{}{}, c.inner.DeleteMessage(callCtx, messageID)
	})
	return err
}

func (c *retryingSessionClient) AcquireJobs(ctx context.Context, requestIDs []int64) ([]int64, error) {
	return retryNetworkCall(ctx, c.retry, "acquire jobs", func(callCtx context.Context) ([]int64, error) {
		return c.inner.AcquireJobs(callCtx, requestIDs)
	})
}

func (c *retryingSessionClient) Session() scaleset.RunnerScaleSetSession {
	return c.inner.Session()
}

func retryNetworkCall[T any](ctx context.Context, retry networkRetry, operation string, call func(context.Context) (T, error)) (T, error) {
	for {
		callCtx, cancelCall := linkedContext(ctx, retry.lifecycle)
		result, err := call(callCtx)
		cancelCall()
		if err == nil {
			return result, err
		}
		if retry.lifecycle.Err() != nil {
			return result, retry.lifecycle.Err()
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if !isTransientNetworkError(err) {
			return result, err
		}
		slog.Warn("runner GitHub request interrupted; retrying", "operation", operation, "error", err, "retry_in", retry.retryDelay)
		waitCtx, cancelWait := linkedContext(ctx, retry.lifecycle)
		err = retry.wait(waitCtx, retry.retryDelay)
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
