package main

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/actions/scaleset"
)

type JITSource interface {
	Generate(context.Context, string) (JITRunner, error)
	Remove(context.Context, int64) error
}

type JITRunner struct {
	Config   string
	ServerID int64
}

type RunnerRuntime interface {
	Start(context.Context, Runner) (<-chan error, error)
	Remove(context.Context, string) error
}

type Scaler struct {
	maxRunners int
	jit        JITSource
	runtime    RunnerRuntime
	newName    func() string
	runners    map[string]activeRunner
}

type activeRunner struct {
	serverID int64
	exit     <-chan error
}

func NewScaler(maxRunners int, jit JITSource, runtime RunnerRuntime, newName func() string) *Scaler {
	return &Scaler{
		maxRunners: maxRunners,
		jit:        jit,
		runtime:    runtime,
		newName:    newName,
		runners:    make(map[string]activeRunner),
	}
}

func (s *Scaler) HandleDesiredRunnerCount(ctx context.Context, desired int) (int, error) {
	for name, runner := range s.runners {
		select {
		case <-runner.exit:
			if err := s.runtime.Remove(ctx, name); err != nil {
				return len(s.runners), fmt.Errorf("remove exited runner %q: %w", name, err)
			}
			if err := s.jit.Remove(ctx, runner.serverID); err != nil {
				return len(s.runners), fmt.Errorf("remove exited server runner %q: %w", name, err)
			}
			delete(s.runners, name)
		default:
		}
	}

	target := min(desired, s.maxRunners)
	for len(s.runners) < target {
		name := s.newName()
		jitRunner, err := s.jit.Generate(ctx, name)
		if err != nil {
			return len(s.runners), fmt.Errorf("generate JIT config: %w", err)
		}
		exit, err := s.runtime.Start(ctx, Runner{Name: name, JITConfig: jitRunner.Config})
		jitRunner.Config = ""
		if err != nil {
			launchErr := fmt.Errorf("start runner %q: %w", name, err)
			if removeErr := s.jit.Remove(ctx, jitRunner.ServerID); removeErr != nil {
				return len(s.runners), errors.Join(launchErr, fmt.Errorf("remove unstarted server runner %q: %w", name, removeErr))
			}
			return len(s.runners), launchErr
		}
		s.runners[name] = activeRunner{serverID: jitRunner.ServerID, exit: exit}
	}
	return len(s.runners), nil
}

func (s *Scaler) HandleJobCompleted(ctx context.Context, job *scaleset.JobCompleted) error {
	if job == nil {
		return nil
	}
	if _, exists := s.runners[job.RunnerName]; !exists {
		return nil
	}
	if err := s.runtime.Remove(ctx, job.RunnerName); err != nil {
		return fmt.Errorf("remove completed runner %q: %w", job.RunnerName, err)
	}
	delete(s.runners, job.RunnerName)
	return nil
}

func (s *Scaler) HandleJobStarted(_ context.Context, _ *scaleset.JobStarted) error {
	return nil
}

func (s *Scaler) Shutdown(ctx context.Context) error {
	names := make([]string, 0, len(s.runners))
	for name := range s.runners {
		names = append(names, name)
	}
	sort.Strings(names)

	var cleanupErr error
	for _, name := range names {
		runner := s.runners[name]
		if err := s.runtime.Remove(ctx, name); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove local runner %q: %w", name, err))
		}
		if err := s.jit.Remove(ctx, runner.serverID); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove server runner %q: %w", name, err))
		}
		delete(s.runners, name)
	}
	return cleanupErr
}
