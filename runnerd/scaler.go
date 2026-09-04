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
	RemoveByName(context.Context, string) error
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
	pending    map[string]struct{}
}

type activeRunner struct {
	serverID int64
	exit     <-chan error
	started  bool
}

func NewScaler(maxRunners int, jit JITSource, runtime RunnerRuntime, newName func() string) *Scaler {
	return &Scaler{
		maxRunners: maxRunners,
		jit:        jit,
		runtime:    runtime,
		newName:    newName,
		runners:    make(map[string]activeRunner),
		pending:    make(map[string]struct{}),
	}
}

func (s *Scaler) HandleDesiredRunnerCount(ctx context.Context, desired int) (int, error) {
	for name, runner := range s.runners {
		select {
		case exitErr := <-runner.exit:
			if exitErr == nil && runner.started {
				runner.exit = nil
				s.runners[name] = runner
				continue
			}
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
	if len(s.runners) > target {
		names := make([]string, 0, len(s.runners))
		for name, runner := range s.runners {
			if !runner.started {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			if len(s.runners) <= target {
				break
			}
			runner := s.runners[name]
			if err := s.runtime.Remove(ctx, name); err != nil {
				return len(s.runners), fmt.Errorf("remove surplus runner %q: %w", name, err)
			}
			if err := s.jit.Remove(ctx, runner.serverID); err != nil {
				return len(s.runners), fmt.Errorf("remove surplus server runner %q: %w", name, err)
			}
			delete(s.runners, name)
		}
	}
	for len(s.runners) < target {
		name := s.newName()
		s.pending[name] = struct{}{}
		jitRunner, err := s.jit.Generate(ctx, name)
		if err != nil {
			return len(s.runners), fmt.Errorf("generate JIT config: %w", err)
		}
		delete(s.pending, name)
		s.runners[name] = activeRunner{serverID: jitRunner.ServerID}
		exit, err := s.runtime.Start(ctx, Runner{Name: name, JITConfig: jitRunner.Config})
		jitRunner.Config = ""
		if err != nil {
			launchErr := fmt.Errorf("start runner %q: %w", name, err)
			if removeErr := s.jit.Remove(ctx, jitRunner.ServerID); removeErr != nil {
				return len(s.runners), errors.Join(launchErr, fmt.Errorf("remove unstarted server runner %q: %w", name, removeErr))
			}
			delete(s.runners, name)
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

func (s *Scaler) HandleJobStarted(_ context.Context, job *scaleset.JobStarted) error {
	if job == nil {
		return nil
	}
	runner, exists := s.runners[job.RunnerName]
	if !exists {
		return nil
	}
	runner.started = true
	s.runners[job.RunnerName] = runner
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
		if err := s.runtime.Remove(ctx, name); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove local runner %q: %w", name, err))
		}
	}
	pendingNames := make([]string, 0, len(s.pending))
	for name := range s.pending {
		pendingNames = append(pendingNames, name)
	}
	sort.Strings(pendingNames)
	for _, name := range pendingNames {
		if err := s.jit.RemoveByName(ctx, name); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("reconcile pending server runner %q: %w", name, err))
		}
		delete(s.pending, name)
	}
	for _, name := range names {
		runner := s.runners[name]
		if err := s.jit.Remove(ctx, runner.serverID); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove server runner %q: %w", name, err))
		}
		delete(s.runners, name)
	}
	return cleanupErr
}
