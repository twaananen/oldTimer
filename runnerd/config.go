package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
)

type Config struct {
	RegistrationURL string
	ScaleSetName    string
	OwnerLabel      string
	ImageTag        string
	MaxRunners      int
	AppClientID     string
	InstallationID  int64
	PrivateKey      string
}

func LoadConfig(getenv func(string) string, readFile func(string) ([]byte, error)) (Config, error) {
	config := Config{
		RegistrationURL: "https://github.com/FullPotatoStudios/Aeons",
		ScaleSetName:    "aeons-oldtimer-linux-x64",
		OwnerLabel:      "oldtimer",
		ImageTag:        getenv("AEONS_RUNNERD_IMAGE_TAG"),
		MaxRunners:      4,
		AppClientID:     getenv("AEONS_RUNNERD_APP_CLIENT_ID"),
	}
	if config.ImageTag == "" {
		return Config{}, errors.New("AEONS_RUNNERD_IMAGE_TAG is required")
	}
	if config.AppClientID == "" {
		return Config{}, errors.New("AEONS_RUNNERD_APP_CLIENT_ID is required")
	}

	installationID, err := strconv.ParseInt(getenv("AEONS_RUNNERD_APP_INSTALLATION_ID"), 10, 64)
	if err != nil || installationID <= 0 {
		return Config{}, errors.New("AEONS_RUNNERD_APP_INSTALLATION_ID must be a positive integer")
	}
	config.InstallationID = installationID

	credentialDirectory := getenv("CREDENTIALS_DIRECTORY")
	if credentialDirectory == "" {
		return Config{}, errors.New("CREDENTIALS_DIRECTORY is required")
	}
	privateKey, err := readFile(filepath.Join(credentialDirectory, "github-app-key"))
	if err != nil {
		return Config{}, fmt.Errorf("read GitHub App private key credential: %w", err)
	}
	if len(privateKey) == 0 {
		return Config{}, errors.New("GitHub App private key credential is empty")
	}
	config.PrivateKey = string(privateKey)
	return config, nil
}
