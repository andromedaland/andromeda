// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package deno

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"

	"github.com/pkg/errors"
)

const CDN_HOST = "cdn.deno.land"
const API_HOST = "api.deno.land"
const PREFIX_LENGTH = len("https://deno.land/x/")

type DenoLandCrawler interface {
	IterateModules() (chan Module, chan error)
}

func NewDenoLandInstrumentedCrawler() DenoLandCrawler {
	c := NewInstrumentedCrawler()
	return c.(DenoLandCrawler)
}

type ApiResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
}

type Module struct {
	Name     string
	Versions map[string][]directoryListing
}

type simpleModuleList []string

type versions struct {
	Latest   string   `json:"latest"`
	Versions []string `json:"versions"`
}

type meta struct {
	UploadedAt       string             `json:"uploaded_at"`
	DirectoryListing []directoryListing `json:"directory_listing"`
}

type directoryListing struct {
	Path string `json:"path"`
	Size int    `json:"size"`
	Type string `json:"type"`
}

func (c *crawler) IterateModules() (chan Module, chan error) {
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

		// TODO(wperron): remove slicing before v1
		list = list[:100]
		wg := sync.WaitGroup{}
		for _, mod := range list {
			wg.Add(1)

			// launching this goroutine floods the runtime with as many goroutines
			// as there are modules on deno.land/x, which are then resolved one
			// by one as the process goes on. This isn't necessarily bad (cpu
			// usage tends to stay low) but should be kept in mind.
			func(mod string, wg *sync.WaitGroup) {
				v, err := c.listModuleVersions(mod)
				if err != nil {
					errs <- err
					return
				}

				versionMap := make(map[string][]directoryListing)

				for _, ver := range v.Versions {
					dir, err := c.getModuleVersionDirectoryListing(mod, ver)
					if err != nil {
						errs <- err
					}

					dir = stripUselessEntries(dir)
					versionMap[ver] = dir
				}

				out <- Module{
					Name:     mod,
					Versions: versionMap,
				}
				wg.Done()
			}(mod, &wg)
		}
		wg.Wait()

		close(out)
		close(errs)
	}()

	return out, errs
}

func (c *crawler) listAllModules() (simpleModuleList, error) {
	u := url.URL{
		Scheme:   "https",
		Host:     API_HOST,
		Path:     "modules",
		RawQuery: "simple=1",
	}
	req, _ := http.NewRequest("GET", u.String(), nil)

	resp, err := c.DoRequest(req)
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

func (c *crawler) listModuleVersions(mod string) (versions, error) {
	u := url.URL{
		Scheme: "https",
		Host:   CDN_HOST,
		Path:   fmt.Sprintf("%s/meta/versions.json", mod),
	}
	req, _ := http.NewRequest("GET", u.String(), nil)

	resp, err := c.DoRequest(req)
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

func (c *crawler) getModuleVersionDirectoryListing(mod, version string) ([]directoryListing, error) {
	u := url.URL{
		Scheme: "https",
		Host:   CDN_HOST,
		Path:   fmt.Sprintf("%s/versions/%s/meta/meta.json", mod, version),
	}
	req, _ := http.NewRequest("GET", u.String(), nil)

	resp, err := c.DoRequest(req)
	if err != nil {
		return []directoryListing{}, errors.Errorf("failed to get directory listing for %s@%s: %s", mod, version, err)
	}
	defer resp.Body.Close()

	var m meta
	body, err := ioutil.ReadAll(resp.Body)
	err = json.Unmarshal(body, &m)
	if err != nil {
		return []directoryListing{}, errors.Errorf("failed to unmarshal response body: %s", err)
	}
	return m.DirectoryListing, nil
}

// Since we only care about source code files, filter out
// directories and non-source code files. There is also a
// special case for README.md to support fulltext search on
// the module's documentation
func stripUselessEntries(dir []directoryListing) []directoryListing {
	for i := 0; i < len(dir); {
		if dir[i].Type == "dir" {
			dir = append(dir[:i], dir[i+1:]...)
			continue
		}
		ext := filepath.Ext(dir[i].Path)
		basename := filepath.Base(dir[i].Path)
		if ext != ".js" && ext != ".ts" && ext != ".jsx" && ext != ".tsx" && basename != "README.md" {
			dir = append(dir[:i], dir[i+1:]...)
			continue
		}
		i++
	}
	return dir
}
