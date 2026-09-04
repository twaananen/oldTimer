package main

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/actions/scaleset"
	"github.com/google/uuid"
)

func TestRetryingSessionClientPreservesSessionAcrossNetworkFailures(t *testing.T) {
	wantMessage := &scaleset.RunnerScaleSetMessage{MessageID: 42}
	wantSession := scaleset.RunnerScaleSetSession{SessionID: uuid.New()}
	networkErr := &net.DNSError{Err: "network is unreachable", Name: "broker.actions.githubusercontent.com", IsTemporary: true}
	inner := &fakeSessionClient{
		message:       wantMessage,
		session:       wantSession,
		getErrors:     []error{context.DeadlineExceeded},
		deleteErrors:  []error{networkErr},
		acquireErrors: []error{networkErr},
	}
	waits := 0
	client := newRetryingSessionClient(context.Background(), inner, time.Second, func(context.Context, time.Duration) error {
		waits++
		return nil
	})
	ctx := context.Background()

	message, err := client.GetMessage(ctx, 0, 4)
	if err != nil || message != wantMessage {
		t.Fatalf("GetMessage = (%v, %v), want (%v, nil)", message, err, wantMessage)
	}
	if err := client.DeleteMessage(ctx, 42); err != nil {
		t.Fatalf("DeleteMessage returned an error: %v", err)
	}
	acquired, err := client.AcquireJobs(ctx, []int64{7})
	if err != nil {
		t.Fatalf("AcquireJobs returned an error: %v", err)
	}
	if !reflect.DeepEqual(acquired, []int64{7}) {
		t.Fatalf("AcquireJobs = %v, want [7]", acquired)
	}
	if got := client.Session(); got.SessionID != wantSession.SessionID {
		t.Fatalf("Session ID = %v, want %v", got.SessionID, wantSession.SessionID)
	}
	if inner.getCalls != 2 || inner.deleteCalls != 2 || inner.acquireCalls != 2 || waits != 3 {
		t.Fatalf("calls get=%d delete=%d acquire=%d waits=%d, want 2/2/2/3", inner.getCalls, inner.deleteCalls, inner.acquireCalls, waits)
	}
}

func TestRetryingSessionClientReturnsPermanentFailure(t *testing.T) {
	wantErr := errors.New("invalid message")
	inner := &fakeSessionClient{getErrors: []error{wantErr}}
	client := newRetryingSessionClient(context.Background(), inner, time.Second, func(context.Context, time.Duration) error {
		t.Fatal("wait called for a permanent error")
		return nil
	})

	_, err := client.GetMessage(context.Background(), 0, 4)
	if !errors.Is(err, wantErr) {
		t.Fatalf("GetMessage error = %v, want %v", err, wantErr)
	}
	if inner.getCalls != 1 {
		t.Fatalf("GetMessage called %d times, want 1", inner.getCalls)
	}
}

func TestRetryingSessionClientStopsUncancelableMessageHandlingAtShutdown(t *testing.T) {
	lifecycle, stop := context.WithCancel(context.Background())
	inner := &blockingSessionClient{started: make(chan struct{})}
	client := newRetryingSessionClient(lifecycle, inner, time.Second, waitForRetry)
	result := make(chan error, 1)
	go func() {
		result <- client.DeleteMessage(context.WithoutCancel(lifecycle), 42)
	}()
	<-inner.started
	stop()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("DeleteMessage error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DeleteMessage did not stop with the daemon lifecycle")
	}
}

type fakeSessionClient struct {
	message       *scaleset.RunnerScaleSetMessage
	session       scaleset.RunnerScaleSetSession
	getErrors     []error
	deleteErrors  []error
	acquireErrors []error
	getCalls      int
	deleteCalls   int
	acquireCalls  int
}

func (f *fakeSessionClient) GetMessage(context.Context, int, int) (*scaleset.RunnerScaleSetMessage, error) {
	f.getCalls++
	if len(f.getErrors) > 0 {
		err := f.getErrors[0]
		f.getErrors = f.getErrors[1:]
		return nil, err
	}
	return f.message, nil
}

func (f *fakeSessionClient) DeleteMessage(context.Context, int) error {
	f.deleteCalls++
	if len(f.deleteErrors) > 0 {
		err := f.deleteErrors[0]
		f.deleteErrors = f.deleteErrors[1:]
		return err
	}
	return nil
}

func (f *fakeSessionClient) AcquireJobs(_ context.Context, requestIDs []int64) ([]int64, error) {
	f.acquireCalls++
	if len(f.acquireErrors) > 0 {
		err := f.acquireErrors[0]
		f.acquireErrors = f.acquireErrors[1:]
		return nil, err
	}
	return requestIDs, nil
}

func (f *fakeSessionClient) Session() scaleset.RunnerScaleSetSession {
	return f.session
}

type blockingSessionClient struct {
	started chan struct{}
}

func (c *blockingSessionClient) GetMessage(ctx context.Context, _, _ int) (*scaleset.RunnerScaleSetMessage, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *blockingSessionClient) DeleteMessage(ctx context.Context, _ int) error {
	close(c.started)
	<-ctx.Done()
	return ctx.Err()
}

func (c *blockingSessionClient) AcquireJobs(ctx context.Context, _ []int64) ([]int64, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *blockingSessionClient) Session() scaleset.RunnerScaleSetSession {
	return scaleset.RunnerScaleSetSession{}
}
