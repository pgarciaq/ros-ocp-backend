package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const noUserConfigEnv = "ROBNE_NO_USER_CONFIG"

type overlayEnv struct {
	Home          string
	XDGConfigHome string
	Cwd           string
	NoUser        bool
}

func overlayEnvFromOS(noUserFlag bool) (overlayEnv, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return overlayEnv{}, fmt.Errorf("home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return overlayEnv{}, fmt.Errorf("working directory: %w", err)
	}
	noUser := noUserFlag
	if v := os.Getenv(noUserConfigEnv); v == "1" || v == "true" || v == "yes" {
		noUser = true
	}
	return overlayEnv{
		Home:          home,
		XDGConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		Cwd:           cwd,
		NoUser:        noUser,
	}, nil
}

func firstExistingUserFile(env overlayEnv, xdgName, homeDotName string) string {
	if env.NoUser {
		return ""
	}
	var candidates []string
	if env.XDGConfigHome != "" {
		candidates = append(candidates, filepath.Join(env.XDGConfigHome, "robne", xdgName))
	}
	if env.Home != "" {
		candidates = append(candidates, filepath.Join(env.Home, ".config", "robne", xdgName))
		candidates = append(candidates, filepath.Join(env.Home, homeDotName))
	}
	for _, p := range candidates {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
