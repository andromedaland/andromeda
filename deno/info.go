// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package deno

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"os/exec"
	"syscall"
)

// DenoInfo is the in-memory representation of the output of `deno info --json`
type DenoInfo struct {
	TotalSize int                  `json:"totalSize"`
	Module    string               `json:"module"`
	Map       *string              `json:"map"`
	Compiled  *string              `json:"compiled"`
	DepCount  int                  `json:"depCount"`
	FileType  string               `json:"fileType"`
	Files     map[string]FileEntry `json:"files"`
}

// FileEntry in the Files map of DenoInfo
type FileEntry struct {
	Deps []string `json:"deps"`
	Size int      `json:"size"`
}

// Exists checks whether the `deno` executable is in path
func Exists() bool {
	path, err := exec.LookPath("deno")
	if err != nil {
		log.Println(err)
		return false
	}

	if path == "" {
		return false
	}

	return true
}

// ExecInfo executes `deno info` as a subcommand and returns the DenoInfo struct
// that it outputs
func ExecInfo(ctx context.Context, target url.URL) (DenoInfo, error) {
	cmd := exec.Command("deno", "info", "--unstable", "--json", target.String())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return DenoInfo{}, err
	}
	if err := cmd.Start(); err != nil {
		return DenoInfo{}, err
	}
	var info DenoInfo
	if err := json.NewDecoder(stdout).Decode(&info); err != nil {
		return DenoInfo{}, err
	}

	errs := make(chan error)
	go func() {
		errs <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		log.Println("received cancel signal, closing ExecInfo")
		cmd.Process.Signal(syscall.SIGTERM)
		return DenoInfo{}, nil
	case err := <-errs:
		if err != nil {
			return DenoInfo{}, err
		}
	}

	return info, nil
}
