package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/actions/scaleset"
)

func TestScaleSetJITSourcePreservesRunnerIDAndRemovalIsIdempotent(t *testing.T) {
	client := &fakeJITClient{}
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

type fakeJITClient struct{}

func (*fakeJITClient) GenerateJitRunnerConfig(_ context.Context, setting *scaleset.RunnerScaleSetJitRunnerSetting, scaleSetID int) (*scaleset.RunnerScaleSetJitRunnerConfig, error) {
	if setting.Name != "aeons-oldtimer-0123456789ab" || scaleSetID != 42 {
		return nil, fmt.Errorf("unexpected JIT request")
	}
	return &scaleset.RunnerScaleSetJitRunnerConfig{
		Runner:           &scaleset.RunnerReference{ID: 123},
		EncodedJITConfig: "encoded-jit",
	}, nil
}

func (*fakeJITClient) RemoveRunner(_ context.Context, _ int64) error {
	return fmt.Errorf("already gone: %w", scaleset.RunnerNotFoundError)
}
