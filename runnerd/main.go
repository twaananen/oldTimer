package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
)

var (
	version   = "dev"
	commitSHA = "unknown"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("runner daemon stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	config, err := LoadConfig(os.Getenv, os.ReadFile)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	pool := PodmanPool{
		OwnerLabel: config.OwnerLabel,
		CPUs:       "2",
		Memory:     "8g",
		PIDsLimit:  2048,
		WorkSize:   "4g",
		TmpfsSize:  "1g",
		Lifecycle:  ctx,
		RunTimeout: 45 * time.Minute,
	}
	imageID, err := pool.ResolveImage(ctx, config.ImageTag)
	if err != nil {
		return fmt.Errorf("runner image preflight: %w", err)
	}
	pool.Image = imageID
	if err := pool.CleanupOwned(ctx); err != nil {
		return fmt.Errorf("startup runner cleanup: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := pool.CleanupOwned(cleanupCtx); err != nil {
			slog.Error("shutdown runner cleanup failed", "error", err)
		}
	}()

	client, err := scaleset.NewClientWithGitHubApp(scaleset.ClientWithGitHubAppConfig{
		GitHubConfigURL: config.RegistrationURL,
		GitHubAppAuth: scaleset.GitHubAppAuth{
			ClientID:       config.AppClientID,
			InstallationID: config.InstallationID,
			PrivateKey:     config.PrivateKey,
		},
		SystemInfo: systemInfo(0),
	})
	if err != nil {
		return fmt.Errorf("create scale-set client: %w", err)
	}

	scaleSet, err := EnsureScaleSet(ctx, client, scaleset.RunnerScaleSet{
		Name:          config.ScaleSetName,
		RunnerGroupID: 1,
		Labels:        []scaleset.Label{{Name: config.ScaleSetName}},
		RunnerSetting: scaleset.RunnerSetting{DisableUpdate: true},
	})
	if err != nil {
		return err
	}
	client.SetSystemInfo(systemInfo(scaleSet.ID))

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read hostname: %w", err)
	}
	session, err := client.MessageSessionClient(ctx, scaleSet.ID, hostname)
	if err != nil {
		return fmt.Errorf("create scale-set message session: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := session.Close(closeCtx); err != nil {
			slog.Error("message-session cleanup failed", "error", err)
		}
	}()

	scaler := NewScaler(
		config.MaxRunners,
		ScaleSetJITSource{Client: client, ScaleSetID: scaleSet.ID},
		pool,
		newRunnerName,
	)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := scaler.Shutdown(cleanupCtx); err != nil {
			slog.Error("runner registration cleanup failed", "error", err)
		}
	}()
	messageListener, err := listener.New(session, listener.Config{
		ScaleSetID: scaleSet.ID,
		MaxRunners: config.MaxRunners,
		Logger:     slog.Default().WithGroup("listener"),
	})
	if err != nil {
		return fmt.Errorf("create scale-set listener: %w", err)
	}

	slog.Info("runner daemon ready", "scale_set", config.ScaleSetName, "max_runners", config.MaxRunners, "image", imageID)
	if err := messageListener.Run(ctx, scaler); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("run scale-set listener: %w", err)
	}
	return nil
}

func systemInfo(scaleSetID int) scaleset.SystemInfo {
	return scaleset.SystemInfo{
		System:     "aeons-runnerd",
		Subsystem:  "podman",
		Version:    version,
		CommitSHA:  commitSHA,
		ScaleSetID: scaleSetID,
	}
}

func newRunnerName() string {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		panic(fmt.Sprintf("generate runner name: %v", err))
	}
	return "aeons-oldtimer-" + hex.EncodeToString(suffix[:])
}

var _ listener.Scaler = (*Scaler)(nil)
