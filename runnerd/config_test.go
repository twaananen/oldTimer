package main

import (
	"reflect"
	"testing"
)

func TestLoadConfigReadsOnlyGitHubAppCredential(t *testing.T) {
	env := map[string]string{
		"AEONS_RUNNERD_APP_CLIENT_ID":       "Iv23.client",
		"AEONS_RUNNERD_APP_INSTALLATION_ID": "12345",
		"CREDENTIALS_DIRECTORY":             "/run/credentials/aeons-runnerd.service",
	}
	getenv := func(name string) string { return env[name] }
	readFile := func(path string) ([]byte, error) {
		if path != "/run/credentials/aeons-runnerd.service/github-app-key" {
			t.Fatalf("read unexpected credential path %q", path)
		}
		return []byte("private-key\n"), nil
	}

	got, err := LoadConfig(getenv, readFile)
	if err != nil {
		t.Fatalf("LoadConfig returned an error: %v", err)
	}
	want := Config{
		RegistrationURL: "https://github.com/FullPotatoStudios/Aeons",
		ScaleSetName:    "aeons-oldtimer-linux-x64",
		OwnerLabel:      "oldtimer",
		ImageTag:        "localhost/aeons-actions-runner:2.337.0-1",
		MaxRunners:      4,
		AppClientID:     "Iv23.client",
		InstallationID:  12345,
		PrivateKey:      "private-key\n",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config %#v, want %#v", got, want)
	}
}
