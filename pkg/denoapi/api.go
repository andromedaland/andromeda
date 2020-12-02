package denoapi

import (
	"encoding/json"
	"github.com/pkg/errors"
	"io/ioutil"
	"net/http"
)
import "net/url"

const CDN_HOST = "cdn.deno.land"
const API_HOST = "api.deno.land"

type ApiResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
}

type SimpleModuleList []string

type Overview struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StarCount   int    `json:"star_count"`
}

type Versions struct {
	Latest   string   `json:"latest"`
	Versions []string `json:"versions"`
}

type Meta struct {
	UploadedAt       string           `json:"uploaded_at"`
	DirectoryListing DirectoryListing `json:"directory_listing"`
}

type DirectoryListing struct {
	Path string `json:"path"`
	Size int    `json:"size"`
	Type string `json:"type"`
}

type DepsV2 struct {
	Graph NodeGraph `json:"graph"`
}

type NodeGraph struct {
	Nodes map[string]Node `json:"nodes"`
}

type Node struct {
	Size int      `json:"size"`
	Deps []string `json:"deps"`
}

func ListAllModules() (SimpleModuleList, error) {
	u := url.URL{
		Scheme:   "https",
		Host:     API_HOST,
		Path:     "modules",
		RawQuery: "simple=1",
	}

	resp, err := http.Get(u.String())
	if err != nil {
		return SimpleModuleList{}, errors.Errorf("failed to get simple list of modules: %s", err)
	}

	var moduleList SimpleModuleList
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	//log.Printf("raw body: %s\n", string(body))
	err = json.Unmarshal(body, &moduleList)
	if err != nil {
		return moduleList, errors.Errorf("failed to unmarshal response body: %s", err)
	}
	return moduleList, nil
}
