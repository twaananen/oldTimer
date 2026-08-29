package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/actions/scaleset"
)

func TestEnsureScaleSetAdoptsAndUpdatesExistingSet(t *testing.T) {
	admin := &fakeScaleSetAdmin{existing: &scaleset.RunnerScaleSet{
		ID:            42,
		Name:          "aeons-oldtimer-linux-x64",
		RunnerGroupID: 1,
	}}
	want := scaleset.RunnerScaleSet{
		Name:          "aeons-oldtimer-linux-x64",
		RunnerGroupID: 1,
		Labels:        []scaleset.Label{{Name: "aeons-oldtimer-linux-x64"}},
		RunnerSetting: scaleset.RunnerSetting{DisableUpdate: true},
	}

	got, err := EnsureScaleSet(context.Background(), admin, want)
	if err != nil {
		t.Fatalf("EnsureScaleSet returned an error: %v", err)
	}
	if got.ID != 42 {
		t.Fatalf("scale set ID = %d, want 42", got.ID)
	}
	if admin.created != nil {
		t.Fatal("created a duplicate scale set")
	}
	if !reflect.DeepEqual(admin.updated, &want) {
		t.Fatalf("updated scale set %#v, want %#v", admin.updated, &want)
	}
}

type fakeScaleSetAdmin struct {
	existing *scaleset.RunnerScaleSet
	created  *scaleset.RunnerScaleSet
	updated  *scaleset.RunnerScaleSet
}

func (a *fakeScaleSetAdmin) GetRunnerScaleSet(_ context.Context, _ int, _ string) (*scaleset.RunnerScaleSet, error) {
	return a.existing, nil
}

func (a *fakeScaleSetAdmin) CreateRunnerScaleSet(_ context.Context, set *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error) {
	a.created = set
	created := *set
	created.ID = 99
	return &created, nil
}

func (a *fakeScaleSetAdmin) UpdateRunnerScaleSet(_ context.Context, id int, set *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error) {
	a.updated = set
	updated := *set
	updated.ID = id
	return &updated, nil
}
