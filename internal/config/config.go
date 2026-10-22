// Package config reads the existing T3oxide environment profile format.
// Credentials belong to the host adapter and never enter a client profile.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const MaxConfigBytes = 64 * 1024

type Environment struct {
	ID             string  `json:"id"`
	Label          string  `json:"label"`
	EnvironmentID  string  `json:"environmentId"`
	SSHDestination *string `json:"sshDestination"`
	Node           string  `json:"node"`
	Adapter        string  `json:"adapter"`
	HostConfig     string  `json:"hostConfig"`
	WebURL         *string `json:"webURL,omitempty"`
	Enabled        bool    `json:"enabled"`
}

var sshAlias = regexp.MustCompile(`^[A-Za-z0-9_.@-]+$`)

func (e Environment) Validate() error {
	if e.ID == "" || e.EnvironmentID == "" || len(e.ID) > 4096 || len(e.EnvironmentID) > 4096 || strings.IndexFunc(e.ID+e.EnvironmentID, func(r rune) bool { return r < 32 }) >= 0 {
		return errors.New("environment needs a nonempty profile and environment identity")
	}
	for _, path := range []string{e.Node, e.Adapter, e.HostConfig} {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\n\r") {
			return fmt.Errorf("environment %q needs absolute host-local executable and configuration paths", e.Label)
		}
	}
	if e.SSHDestination != nil && (strings.HasPrefix(*e.SSHDestination, "-") || !sshAlias.MatchString(*e.SSHDestination)) {
		return fmt.Errorf("environment %q needs an existing SSH alias", e.Label)
	}
	if e.WebURL != nil {
		u, err := url.Parse(*e.WebURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return errors.New("full T3 URL must be HTTP(S), without credentials, query, or fragment")
		}
	}
	return nil
}

// Command returns an executable and argv without invoking a local shell. The
// one remote SSH command uses POSIX single-quote escaping for all host paths.
func (e Environment) Command() (string, []string) {
	if e.SSHDestination == nil {
		return e.Node, []string{e.Adapter, "--config", e.HostConfig}
	}
	command := "exec " + shellQuote(e.Node) + " " + shellQuote(e.Adapter) + " --config " + shellQuote(e.HostConfig)
	return "/usr/bin/ssh", []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=8", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=2", "--", *e.SSHDestination, command}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func DefaultDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config/t3oxide"
	}
	return filepath.Join(home, ".config", "t3oxide")
}
func DefaultEnvironmentsPath() string { return filepath.Join(DefaultDirectory(), "environments.json") }

func Load(path string) ([]Environment, error) {
	if path == "" {
		path = DefaultEnvironmentsPath()
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read environments: %w", err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxConfigBytes {
		return nil, errors.New("environment configuration exceeds 64 KiB")
	}
	var saved struct {
		Version      int           `json:"version"`
		Environments []Environment `json:"environments"`
	}
	if err = json.Unmarshal(b, &saved); err != nil {
		return nil, fmt.Errorf("decode environments: %w", err)
	}
	if saved.Version != 1 || len(saved.Environments) > 8 {
		return nil, errors.New("expected version 1 and at most eight environments")
	}
	ids, identities := map[string]bool{}, map[string]bool{}
	var enabled []Environment
	for _, e := range saved.Environments {
		if err = e.Validate(); err != nil {
			return nil, err
		}
		if ids[e.ID] || identities[e.EnvironmentID] {
			return nil, errors.New("environment profile IDs and environment identities must be unique")
		}
		ids[e.ID], identities[e.EnvironmentID] = true, true
		if e.Enabled {
			enabled = append(enabled, e)
		}
	}
	return enabled, nil
}
