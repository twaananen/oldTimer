package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const pinnedRunnerImage = "ghcr.io/actions/actions-runner@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestPodmanPlanIsHardenedAndBounded(t *testing.T) {
	pool := PodmanPool{
		Image:      pinnedRunnerImage,
		OwnerLabel: "oldtimer",
		CPUs:       "2",
		Memory:     "8g",
		PIDsLimit:  2048,
		WorkSize:   "4g",
		TmpfsSize:  "1g",
	}

	got, err := pool.Plan(Runner{
		Name:      "aeons-oldtimer-0123456789ab",
		JITConfig: "one-job-secret",
	})
	if err != nil {
		t.Fatalf("Plan returned an error: %v", err)
	}

	want := Command{
		Path: "podman",
		Env: map[string]string{
			"ACTIONS_RUNNER_INPUT_JITCONFIG":     "one-job-secret",
			"ACTIONS_RUNNER_PRINT_LOG_TO_STDOUT": "1",
			"HOME":                               "/runner",
		},
		Args: []string{
			"run", "--rm",
			"--name", "aeons-oldtimer-0123456789ab",
			"--label", "io.aeons.runnerd.owner=oldtimer",
			"--label", "io.aeons.runnerd.name=aeons-oldtimer-0123456789ab",
			"--pull=never",
			"--userns=auto:size=8192",
			"--cgroups=split",
			"--cpus=2",
			"--memory=8g",
			"--memory-swap=8g",
			"--pids-limit=2048",
			"--cap-drop=all",
			"--security-opt=no-new-privileges",
			"--network=pasta:--ipv4-only,--no-map-gw",
			"--read-only",
			"--tmpfs=/runner:rw,nosuid,nodev,size=4g,mode=0777",
			"--tmpfs=/tmp:rw,nosuid,nodev,size=1g",
			"--stop-timeout=30",
			"--dns=1.1.1.1",
			"--dns=9.9.9.9",
			"--user=runner",
			"--workdir=/runner",
			"--env", "HOME",
			"--env", "ACTIONS_RUNNER_INPUT_JITCONFIG",
			"--env", "ACTIONS_RUNNER_PRINT_LOG_TO_STDOUT",
			pinnedRunnerImage,
			"/opt/actions-runner/entrypoint.sh",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected Podman command\n got: %#v\nwant: %#v", got, want)
	}
	for _, arg := range got.Args {
		if arg == "one-job-secret" || strings.Contains(arg, "one-job-secret") {
			t.Fatalf("JIT config leaked into process arguments: %q", arg)
		}
	}
}

func TestPodmanPlanRejectsUnsafeInputs(t *testing.T) {
	validPool := PodmanPool{
		Image:      pinnedRunnerImage,
		OwnerLabel: "oldtimer",
		CPUs:       "2",
		Memory:     "8g",
		PIDsLimit:  2048,
		WorkSize:   "4g",
		TmpfsSize:  "1g",
	}
	validRunner := Runner{Name: "aeons-oldtimer-0123456789ab", JITConfig: "secret"}

	tests := map[string]struct {
		pool   PodmanPool
		runner Runner
	}{
		"mutable image tag": {
			pool:   withImage(validPool, "ghcr.io/actions/actions-runner:latest"),
			runner: validRunner,
		},
		"option-like runner name": {
			pool:   validPool,
			runner: Runner{Name: "--privileged", JITConfig: "secret"},
		},
		"arbitrary ownership label": {
			pool:   withOwner(validPool, "oldtimer,io.aeons.escape=true"),
			runner: validRunner,
		},
		"empty JIT config": {
			pool:   validPool,
			runner: Runner{Name: validRunner.Name},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := test.pool.Plan(test.runner); err == nil {
				t.Fatal("Plan accepted unsafe input")
			}
		})
	}
}

func withImage(pool PodmanPool, image string) PodmanPool {
	pool.Image = image
	return pool
}

func withOwner(pool PodmanPool, owner string) PodmanPool {
	pool.OwnerLabel = owner
	return pool
}

func TestPodmanCleanupRemovesOnlyOwnedRunnerNames(t *testing.T) {
	exec := &recordingExecutor{outputs: [][]byte{
		[]byte("aeons-oldtimer-0123456789ab\naeons-oldtimer-abcdef012345\n"),
		nil,
		nil,
	}}
	pool := PodmanPool{OwnerLabel: "oldtimer", Executor: exec}

	if err := pool.CleanupOwned(context.Background()); err != nil {
		t.Fatalf("CleanupOwned returned an error: %v", err)
	}

	want := []Command{
		{
			Path: "podman",
			Args: []string{
				"ps", "--all",
				"--filter", "label=io.aeons.runnerd.owner=oldtimer",
				"--format", "{{.Names}}",
			},
		},
		{Path: "podman", Args: []string{"rm", "--force", "--time=10", "--ignore", "aeons-oldtimer-0123456789ab"}},
		{Path: "podman", Args: []string{"rm", "--force", "--time=10", "--ignore", "aeons-oldtimer-abcdef012345"}},
	}
	if !reflect.DeepEqual(exec.commands, want) {
		t.Fatalf("unexpected cleanup commands\n got: %#v\nwant: %#v", exec.commands, want)
	}
}

func TestPodmanStartAndRemoveExecuteOnlyThePlannedRunner(t *testing.T) {
	exitErr := errors.New("runner exited")
	exit := make(chan error, 1)
	exit <- exitErr
	exec := &recordingExecutor{outputs: [][]byte{nil}, exit: exit}
	pool := PodmanPool{
		Image:      pinnedRunnerImage,
		OwnerLabel: "oldtimer",
		CPUs:       "2",
		Memory:     "8g",
		PIDsLimit:  2048,
		WorkSize:   "4g",
		TmpfsSize:  "1g",
		Executor:   exec,
	}
	runner := Runner{Name: "aeons-oldtimer-0123456789ab", JITConfig: "one-job-secret"}
	planned, err := pool.Plan(runner)
	if err != nil {
		t.Fatalf("Plan returned an error: %v", err)
	}

	processExit, err := pool.Start(context.Background(), runner)
	if err != nil {
		t.Fatalf("Start returned an error: %v", err)
	}
	if got := <-processExit; !errors.Is(got, exitErr) {
		t.Fatalf("process exit = %v, want %v", got, exitErr)
	}
	if err := pool.Remove(context.Background(), runner.Name); err != nil {
		t.Fatalf("Remove returned an error: %v", err)
	}

	want := []Command{
		planned,
		{Path: "podman", Args: []string{"rm", "--force", "--time=10", "--ignore", runner.Name}},
	}
	if !reflect.DeepEqual(exec.commands, want) {
		t.Fatalf("unexpected lifecycle commands\n got: %#v\nwant: %#v", exec.commands, want)
	}
}

func TestPodmanResolveImageReturnsImmutableLocalImageID(t *testing.T) {
	imageID := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	exec := &recordingExecutor{outputs: [][]byte{[]byte(imageID + "\n")}}
	pool := PodmanPool{Executor: exec}

	got, err := pool.ResolveImage(context.Background(), "localhost/aeons-actions-runner:2.337.0-1")
	if err != nil {
		t.Fatalf("ResolveImage returned an error: %v", err)
	}
	if got != imageID {
		t.Fatalf("image ID = %q, want %q", got, imageID)
	}

	want := []Command{{
		Path: "podman",
		Args: []string{"image", "inspect", "--format", "{{.Id}}", "--", "localhost/aeons-actions-runner:2.337.0-1"},
	}}
	if !reflect.DeepEqual(exec.commands, want) {
		t.Fatalf("unexpected image command\n got: %#v\nwant: %#v", exec.commands, want)
	}
}

func TestPodmanLifecycleCancellationStopsAttachedRunner(t *testing.T) {
	lifecycle, cancel := context.WithCancel(context.Background())
	processExit := make(chan error)
	exec := &recordingExecutor{exit: processExit}
	pool := PodmanPool{
		Image:      pinnedRunnerImage,
		OwnerLabel: "oldtimer",
		CPUs:       "2",
		Memory:     "8g",
		PIDsLimit:  2048,
		WorkSize:   "4g",
		TmpfsSize:  "1g",
		Executor:   exec,
		Lifecycle:  lifecycle,
		RunTimeout: 45 * time.Minute,
	}
	runner := Runner{Name: "aeons-oldtimer-0123456789ab", JITConfig: "one-job-secret"}

	exit, err := pool.Start(context.Background(), runner)
	if err != nil {
		t.Fatalf("Start returned an error: %v", err)
	}
	cancel()
	if got := <-exit; !errors.Is(got, context.Canceled) {
		t.Fatalf("managed process exit = %v, want context cancellation", got)
	}

	last := exec.commands[len(exec.commands)-1]
	want := Command{Path: "podman", Args: []string{"rm", "--force", "--time=10", "--ignore", runner.Name}}
	if !reflect.DeepEqual(last, want) {
		t.Fatalf("cancellation command %#v, want %#v", last, want)
	}
}

type recordingExecutor struct {
	commands []Command
	outputs  [][]byte
	exit     <-chan error
}

func (e *recordingExecutor) Output(_ context.Context, command Command) ([]byte, error) {
	e.commands = append(e.commands, command)
	if len(e.outputs) == 0 {
		return nil, nil
	}
	output := e.outputs[0]
	e.outputs = e.outputs[1:]
	return output, nil
}

func (e *recordingExecutor) Start(_ context.Context, command Command) (<-chan error, error) {
	e.commands = append(e.commands, command)
	if e.exit != nil {
		return e.exit, nil
	}
	exit := make(chan error, 1)
	exit <- nil
	return exit, nil
}
