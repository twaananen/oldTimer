package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrRunnerDeadline = errors.New("runner exceeded its maximum lifetime")

const requiredRunnerCgroupParent = "/aeons.slice/aeons-ci.slice/aeons-runnerd.service"

var (
	pinnedImagePattern = regexp.MustCompile(`^(?:[a-z0-9./_-]+@)?sha256:[0-9a-f]{64}$`)
	bareImageIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	imageTagPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9./:_-]{0,254}$`)
	ownerLabelPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,62}$`)
	runnerNamePattern  = regexp.MustCompile(`^aeons-oldtimer-[0-9a-f]{12}$`)
)

type Runner struct {
	Name      string
	JITConfig string
}

type Command struct {
	Path string
	Args []string
	Env  map[string]string
}

type PodmanPool struct {
	Image        string
	OwnerLabel   string
	CgroupParent string
	CPUs         string
	Memory       string
	PIDsLimit    int
	WorkSize     string
	TmpfsSize    string
	Executor     Executor
	Lifecycle    context.Context
	RunTimeout   time.Duration
}

type Executor interface {
	Output(context.Context, Command) ([]byte, error)
	Start(context.Context, Command) (<-chan error, error)
}

type OSExecutor struct{}

func (OSExecutor) Output(ctx context.Context, command Command) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		return output, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return output, err
}

func (OSExecutor) Start(ctx context.Context, command Command) (<-chan error, error) {
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	cmd.Env = os.Environ()
	for name, value := range command.Env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	startErr := cmd.Start()
	clear(cmd.Env)
	cmd.Env = nil
	if startErr != nil {
		return nil, startErr
	}
	exit := make(chan error, 1)
	go func() {
		exit <- cmd.Wait()
		close(exit)
	}()
	return exit, nil
}

func (p PodmanPool) Plan(runner Runner) (Command, error) {
	if !pinnedImagePattern.MatchString(p.Image) {
		return Command{}, errors.New("runner image must be pinned by sha256 digest")
	}
	if !ownerLabelPattern.MatchString(p.OwnerLabel) {
		return Command{}, errors.New("invalid owner label")
	}
	if p.CgroupParent != requiredRunnerCgroupParent {
		return Command{}, errors.New("runner cgroup parent is outside the delegated service")
	}
	if !runnerNamePattern.MatchString(runner.Name) {
		return Command{}, errors.New("invalid runner name")
	}
	if runner.JITConfig == "" {
		return Command{}, errors.New("JIT config is required")
	}

	return Command{
		Path: "podman",
		Env: map[string]string{
			"ACTIONS_RUNNER_INPUT_JITCONFIG":     runner.JITConfig,
			"ACTIONS_RUNNER_PRINT_LOG_TO_STDOUT": "1",
		},
		Args: []string{
			"run", "--rm",
			"--name", runner.Name,
			"--label", "io.aeons.runnerd.owner=" + p.OwnerLabel,
			"--label", "io.aeons.runnerd.name=" + runner.Name,
			"--pull=never",
			"--userns=auto:size=8192",
			"--cgroups=enabled",
			"--cgroup-parent=" + p.CgroupParent,
			"--cpus=" + p.CPUs,
			"--memory=" + p.Memory,
			"--memory-swap=" + p.Memory,
			"--pids-limit=" + strconv.Itoa(p.PIDsLimit),
			"--cap-drop=all",
			"--security-opt=no-new-privileges",
			"--network=pasta:--ipv4-only,--no-map-gw",
			"--read-only",
			"--tmpfs=/runner:rw,nosuid,nodev,size=" + p.WorkSize + ",mode=0777",
			"--tmpfs=/tmp:rw,nosuid,nodev,size=" + p.TmpfsSize,
			"--stop-timeout=30",
			"--dns=1.1.1.1",
			"--dns=9.9.9.9",
			"--user=runner",
			"--workdir=/runner",
			"--env", "HOME=/runner",
			"--env", "ACTIONS_RUNNER_INPUT_JITCONFIG",
			"--env", "ACTIONS_RUNNER_PRINT_LOG_TO_STDOUT",
			p.Image,
			"/opt/actions-runner/entrypoint.sh",
		},
	}, nil
}

func (p PodmanPool) CleanupOwned(ctx context.Context) error {
	if !ownerLabelPattern.MatchString(p.OwnerLabel) {
		return errors.New("invalid owner label")
	}

	output, err := p.executor().Output(ctx, Command{
		Path: "podman",
		Args: []string{
			"ps", "--all",
			"--filter", "label=io.aeons.runnerd.owner=" + p.OwnerLabel,
			"--format", "{{.Names}}",
		},
	})
	if err != nil {
		return fmt.Errorf("list owned runner containers: %w", err)
	}

	for _, name := range strings.Fields(string(output)) {
		if err := p.Remove(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

func (p PodmanPool) Start(ctx context.Context, runner Runner) (<-chan error, error) {
	command, err := p.Plan(runner)
	if err != nil {
		return nil, err
	}
	lifecycle := p.Lifecycle
	if lifecycle == nil {
		lifecycle = ctx
	}
	processExit, err := p.executor().Start(lifecycle, command)
	if err != nil {
		return nil, fmt.Errorf("start runner %q: %w", runner.Name, err)
	}

	managedExit := make(chan error, 1)
	go func() {
		var deadline <-chan time.Time
		var timer *time.Timer
		if p.RunTimeout > 0 {
			timer = time.NewTimer(p.RunTimeout)
			deadline = timer.C
			defer timer.Stop()
		}

		select {
		case err := <-processExit:
			managedExit <- err
		case <-lifecycle.Done():
			managedExit <- errors.Join(lifecycle.Err(), p.stopRunner(runner.Name))
		case <-deadline:
			managedExit <- errors.Join(ErrRunnerDeadline, p.stopRunner(runner.Name))
		}
		close(managedExit)
	}()
	return managedExit, nil
}

func (p PodmanPool) stopRunner(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return p.Remove(ctx, name)
}

func (p PodmanPool) Remove(ctx context.Context, name string) error {
	if !runnerNamePattern.MatchString(name) {
		return fmt.Errorf("refusing to remove invalid owned runner name %q", name)
	}
	if _, err := p.executor().Output(ctx, Command{
		Path: "podman",
		Args: []string{"rm", "--force", "--time=10", "--ignore", name},
	}); err != nil {
		return fmt.Errorf("remove owned runner %q: %w", name, err)
	}
	return nil
}

func (p PodmanPool) ResolveImage(ctx context.Context, tag string) (string, error) {
	if !imageTagPattern.MatchString(tag) {
		return "", errors.New("invalid local runner image tag")
	}
	output, err := p.executor().Output(ctx, Command{
		Path: "podman",
		Args: []string{"image", "inspect", "--format", "{{.Id}}", "--", tag},
	})
	if err != nil {
		return "", fmt.Errorf("inspect local runner image: %w", err)
	}
	id := strings.TrimSpace(string(output))
	if bareImageIDPattern.MatchString(id) {
		id = "sha256:" + id
	}
	if !pinnedImagePattern.MatchString(id) {
		return "", errors.New("local runner image did not resolve to an immutable image ID")
	}
	return id, nil
}

func (p PodmanPool) executor() Executor {
	if p.Executor != nil {
		return p.Executor
	}
	return OSExecutor{}
}
