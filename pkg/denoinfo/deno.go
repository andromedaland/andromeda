// Copyright 2020 William Perron. All rights reserved. MIT License.
package denoinfo

import (
	"encoding/json"
	"net/url"
	"os/exec"
)

type DenoInfo struct {
	TotalSize int                  `json:"totalSize"`
	Module    string               `json:"module"`
	Map       *string              `json:"map"`
	Compiled  *string              `json:"compiled"`
	DepCount  int                  `json:"depCount"`
	FileType  string               `json:"fileType"`
	Files     map[string]FileEntry `json:"files"`
}

type FileEntry struct {
	Deps []string `json:"deps"`
	Size int      `json:"size"`
}

func ExecInfo(target url.URL) (DenoInfo, error) {
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
	if err := cmd.Wait(); err != nil {
		return info, nil
	}
	return info, nil
}
