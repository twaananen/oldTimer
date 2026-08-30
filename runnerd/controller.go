package main

import (
	"context"
	"fmt"

	"github.com/actions/scaleset"
)

type ScaleSetAdmin interface {
	GetRunnerScaleSet(context.Context, int, string) (*scaleset.RunnerScaleSet, error)
	CreateRunnerScaleSet(context.Context, *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error)
	UpdateRunnerScaleSet(context.Context, int, *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error)
}

func EnsureScaleSet(ctx context.Context, admin ScaleSetAdmin, desired scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error) {
	existing, err := admin.GetRunnerScaleSet(ctx, desired.RunnerGroupID, desired.Name)
	if err != nil {
		return nil, fmt.Errorf("get runner scale set: %w", err)
	}
	if existing == nil {
		created, err := admin.CreateRunnerScaleSet(ctx, &desired)
		if err != nil {
			return nil, fmt.Errorf("create runner scale set: %w", err)
		}
		return created, nil
	}
	updated, err := admin.UpdateRunnerScaleSet(ctx, existing.ID, &desired)
	if err != nil {
		return nil, fmt.Errorf("update runner scale set: %w", err)
	}
	return updated, nil
}
