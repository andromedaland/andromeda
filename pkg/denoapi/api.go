// Copyright 2020 William Perron. All rights reserved. MIT License.
package denoapi

import (
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"net/http"
	"sync"
	"time"
)
import "net/url"

const CDN_HOST = "cdn.deno.land"
const API_HOST = "api.deno.land"

type ApiResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
}

type Client struct {
	Transport    *http.Client
	ThrottleRate int // minimal interval wait between requests
	mut          sync.Mutex
	last         time.Time
}

type Module struct {
	Name     string
	Versions versions
}

type simpleModuleList []string

type overview struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StarCount   int    `json:"star_count"`
}

type versions struct {
	Latest   string   `json:"latest"`
	Versions []string `json:"versions"`
}

type meta struct {
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

func NewClient() Client {
	return Client{
		Transport:    http.DefaultClient,
		ThrottleRate: 1,
	}
}

func (c *Client) doRequest(req *http.Request) (*http.Response, error) {
	c.mut.Lock()
	defer c.mut.Unlock()

	time.Sleep(time.Until(c.last.Add(time.Duration(c.ThrottleRate) * time.Second)))
	c.last = time.Now()
	req.Header.Set("User-Agent", "Wperron/Depgraph-v0.1")
	return c.Transport.Do(req)
}

func (c *Client) IterateModules() (chan Module, chan error) {
	out := make(chan Module)
	errs := make(chan error)

	go func() {
		list, err := c.listAllModules()
		if err != nil {
			close(out)
			errs <- errors.Errorf("failed to list all module names: %s", err)
			close(errs)
			return
		}

		i := 0
		for _, mod := range list {
			i++
			if i > 10 {
				break
			}
			versions, err := c.listModuleVersions(mod)
			if err != nil {
				errs <- errors.Errorf("failed to get versions for module %s: %s", mod, err)
				continue
			}

			out <- Module{
				Name:     mod,
				Versions: versions,
			}
		}

		close(out)
		close(errs)
	}()

	return out, errs
}

func (c *Client) listAllModules() (simpleModuleList, error) {
	u := url.URL{
		Scheme:   "https",
		Host:     API_HOST,
		Path:     "modules",
		RawQuery: "simple=1",
	}
	req, _ := http.NewRequest("GET", u.String(), nil)

	resp, err := c.doRequest(req)
	if err != nil {
		return simpleModuleList{}, errors.Errorf("failed to get simple list of modules: %s", err)
	}
	defer resp.Body.Close()

	var moduleList simpleModuleList
	body, err := ioutil.ReadAll(resp.Body)
	err = json.Unmarshal(body, &moduleList)

	if err != nil {
		return moduleList, errors.Errorf("failed to unmarshal response body: %s", err)
	}
	return moduleList, nil
}

func (c *Client) listModuleVersions(mod string) (versions, error) {
	u := url.URL{
		Scheme: "https",
		Host:   CDN_HOST,
		Path:   fmt.Sprintf("%s/meta/versions.json", mod),
	}
	req, _ := http.NewRequest("GET", u.String(), nil)

	resp, err := c.doRequest(req)
	if err != nil {
		return versions{}, errors.Errorf("failed to get versions for module %s: %s\n", mod, err)
	}
	defer resp.Body.Close()

	var ver versions
	body, err := ioutil.ReadAll(resp.Body)
	err = json.Unmarshal(body, &ver)

	if err != nil {
		return ver, errors.Errorf("failed to unmarshal response body: %s", err)
	}
	return ver, nil
}
