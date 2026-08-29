package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/actions/scaleset"
)

func TestScalerCapsDemandAndStartsOneJITRunnerPerSlot(t *testing.T) {
	jit := &fakeJITSource{}
	runtime := &fakeRuntime{}
	next := 0
	scaler := NewScaler(4, jit, runtime, func() string {
		next++
		return fmt.Sprintf("aeons-oldtimer-%012x", next)
	})

	got, err := scaler.HandleDesiredRunnerCount(context.Background(), 6)
	if err != nil {
		t.Fatalf("HandleDesiredRunnerCount returned an error: %v", err)
	}
	if got != 4 {
		t.Fatalf("created %d runners, want 4", got)
	}

	want := []Runner{
		{Name: "aeons-oldtimer-000000000001", JITConfig: "jit-aeons-oldtimer-000000000001"},
		{Name: "aeons-oldtimer-000000000002", JITConfig: "jit-aeons-oldtimer-000000000002"},
		{Name: "aeons-oldtimer-000000000003", JITConfig: "jit-aeons-oldtimer-000000000003"},
		{Name: "aeons-oldtimer-000000000004", JITConfig: "jit-aeons-oldtimer-000000000004"},
	}
	if !reflect.DeepEqual(runtime.started, want) {
		t.Fatalf("unexpected runners\n got: %#v\nwant: %#v", runtime.started, want)
	}
}

func TestScalerCompletionRemovesRunnerOnceAndFreesItsSlot(t *testing.T) {
	jit := &fakeJITSource{}
	runtime := &fakeRuntime{}
	names := []string{"aeons-oldtimer-000000000001", "aeons-oldtimer-000000000002"}
	scaler := NewScaler(1, jit, runtime, func() string {
		name := names[0]
		names = names[1:]
		return name
	})

	if _, err := scaler.HandleDesiredRunnerCount(context.Background(), 1); err != nil {
		t.Fatalf("start first runner: %v", err)
	}
	completed := &scaleset.JobCompleted{RunnerName: "aeons-oldtimer-000000000001"}
	if err := scaler.HandleJobCompleted(context.Background(), completed); err != nil {
		t.Fatalf("complete runner: %v", err)
	}
	if err := scaler.HandleJobCompleted(context.Background(), completed); err != nil {
		t.Fatalf("repeat completion: %v", err)
	}
	if _, err := scaler.HandleDesiredRunnerCount(context.Background(), 1); err != nil {
		t.Fatalf("reuse freed slot: %v", err)
	}

	if want := []string{"aeons-oldtimer-000000000001"}; !reflect.DeepEqual(runtime.removed, want) {
		t.Fatalf("removed runners %v, want %v", runtime.removed, want)
	}
	if got := runtime.started[len(runtime.started)-1].Name; got != "aeons-oldtimer-000000000002" {
		t.Fatalf("replacement runner is %q", got)
	}
}

func TestScalerReplacesRunnerWhenAttachedProcessExits(t *testing.T) {
	jit := &fakeJITSource{}
	runtime := &fakeRuntime{}
	names := []string{"aeons-oldtimer-000000000001", "aeons-oldtimer-000000000002"}
	scaler := NewScaler(1, jit, runtime, func() string {
		name := names[0]
		names = names[1:]
		return name
	})

	if _, err := scaler.HandleDesiredRunnerCount(context.Background(), 1); err != nil {
		t.Fatalf("start first runner: %v", err)
	}
	runtime.exits["aeons-oldtimer-000000000001"] <- errors.New("process exited")
	if _, err := scaler.HandleDesiredRunnerCount(context.Background(), 1); err != nil {
		t.Fatalf("replace exited runner: %v", err)
	}

	if want := []string{"aeons-oldtimer-000000000001"}; !reflect.DeepEqual(runtime.removed, want) {
		t.Fatalf("removed runners %v, want %v", runtime.removed, want)
	}
	if got := runtime.started[len(runtime.started)-1].Name; got != "aeons-oldtimer-000000000002" {
		t.Fatalf("replacement runner is %q", got)
	}
	if want := []int64{1}; !reflect.DeepEqual(jit.removed, want) {
		t.Fatalf("removed server runners %v, want %v", jit.removed, want)
	}
}

func TestScalerAcceptsRepeatedAndOutOfOrderJobStartedEvents(t *testing.T) {
	scaler := NewScaler(1, &fakeJITSource{}, &fakeRuntime{}, func() string {
		return "aeons-oldtimer-000000000001"
	})
	ctx := context.Background()
	unknown := &scaleset.JobStarted{RunnerName: "aeons-oldtimer-ffffffffffff"}
	if err := scaler.HandleJobStarted(ctx, unknown); err != nil {
		t.Fatalf("unknown JobStarted returned an error: %v", err)
	}
	if _, err := scaler.HandleDesiredRunnerCount(ctx, 1); err != nil {
		t.Fatalf("start runner: %v", err)
	}
	known := &scaleset.JobStarted{RunnerName: "aeons-oldtimer-000000000001"}
	if err := scaler.HandleJobStarted(ctx, known); err != nil {
		t.Fatalf("JobStarted returned an error: %v", err)
	}
	if err := scaler.HandleJobStarted(ctx, known); err != nil {
		t.Fatalf("repeated JobStarted returned an error: %v", err)
	}
}

func TestScalerRemovesServerRunnerWhenContainerLaunchFails(t *testing.T) {
	jit := &fakeJITSource{}
	runtime := &fakeRuntime{startErr: errors.New("podman failed")}
	scaler := NewScaler(1, jit, runtime, func() string {
		return "aeons-oldtimer-000000000001"
	})

	if _, err := scaler.HandleDesiredRunnerCount(context.Background(), 1); err == nil {
		t.Fatal("launch failure was accepted")
	}
	if want := []int64{1}; !reflect.DeepEqual(jit.removed, want) {
		t.Fatalf("removed server runners %v, want %v", jit.removed, want)
	}
}

func TestScalerShutdownRemovesLocalAndServerRunners(t *testing.T) {
	jit := &fakeJITSource{}
	runtime := &fakeRuntime{}
	next := 0
	scaler := NewScaler(2, jit, runtime, func() string {
		next++
		return fmt.Sprintf("aeons-oldtimer-%012x", next)
	})
	ctx := context.Background()
	if _, err := scaler.HandleDesiredRunnerCount(ctx, 2); err != nil {
		t.Fatalf("start runners: %v", err)
	}

	if err := scaler.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned an error: %v", err)
	}
	if want := []string{"aeons-oldtimer-000000000001", "aeons-oldtimer-000000000002"}; !reflect.DeepEqual(runtime.removed, want) {
		t.Fatalf("removed local runners %v, want %v", runtime.removed, want)
	}
	if want := []int64{1, 2}; !reflect.DeepEqual(jit.removed, want) {
		t.Fatalf("removed server runners %v, want %v", jit.removed, want)
	}
}

type fakeJITSource struct {
	nextID  int64
	removed []int64
}

func (j *fakeJITSource) Generate(_ context.Context, name string) (JITRunner, error) {
	j.nextID++
	return JITRunner{Config: "jit-" + name, ServerID: j.nextID}, nil
}

func (j *fakeJITSource) Remove(_ context.Context, serverID int64) error {
	j.removed = append(j.removed, serverID)
	return nil
}

type fakeRuntime struct {
	started  []Runner
	removed  []string
	exits    map[string]chan error
	startErr error
}

func (r *fakeRuntime) Start(_ context.Context, runner Runner) (<-chan error, error) {
	if r.startErr != nil {
		return nil, r.startErr
	}
	if r.exits == nil {
		r.exits = make(map[string]chan error)
	}
	r.started = append(r.started, runner)
	exit := make(chan error, 1)
	r.exits[runner.Name] = exit
	return exit, nil
}

func (r *fakeRuntime) Remove(_ context.Context, name string) error {
	r.removed = append(r.removed, name)
	return nil
}
