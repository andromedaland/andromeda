// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package deno

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"

	"github.com/pkg/errors"
)

const CDN_HOST = "cdn.deno.land"
const API_HOST = "api.deno.land"
const PREFIX_LENGTH = len("https://deno.land/x/")

// XQueuedCrawler is a composite type composed of both a Queue and a Crawler
type XQueuedCrawler struct {
	Crawler
	done chan bool
	Queue
}

type apiResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
}

// Module contains the name of the volume and a map of all its versions to all
// the files contained in the module
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

// NewXQueuedCrawler returns an instance of a crawler for https://deno.land with
// a Queue
func NewXQueuedCrawler(q Queue) *XQueuedCrawler {
	return &XQueuedCrawler{
		Crawler: NewInstrumentedCrawler(),
		Queue:   q,
	}
}

// IterateModules asynchronously consumes the queue and sends each Module to a
// channel
func (x *XQueuedCrawler) IterateModules(ctx context.Context) (chan Module, chan error) {
	out := make(chan Module)
	errs := make(chan error)

	go func() {
		// TODO(wperron) add a ctx param to the function and a select statement
		//  to exit the infinite loop
		for {
			select {
			case <-ctx.Done():
				log.Println("received cancel signal, closing IterateModules goroutine")
				close(out)
				close(errs)
				return
			default:
			}

			mod, err := x.Queue.Get()
			if err != nil {
				errs <- err
			} else {
				out <- mod
			}
		}
	}()

	return out, errs
}

var closedchan = make(chan bool)

func init() {
	close(closedchan)
}

// Done returns the done channel of the crawler
func (x *XQueuedCrawler) Done() <-chan bool {
	if x.done == nil {
		x.done = make(chan bool)
	}
	return x.done
}

// Crawl asynchronously crawls https://deno.land and puts each Module in the
// queue to be processed later
func (x *XQueuedCrawler) Crawl(ctx context.Context) chan error {
	errs := make(chan error)

	go func() {
		if x.done == closedchan || x.done == nil {
			x.done = make(chan bool)
		}

		list, err := x.listAllModules()
		if err != nil {
			errs <- err
			close(errs)
			return
		}

		wg := sync.WaitGroup{}
		for mod := range list {
			wg.Add(1)
			go func(mod string, wg *sync.WaitGroup) {
				select {
				case <-ctx.Done():
					wg.Done()
					return
				default:
				}

				v, err := x.listModuleVersions(mod)
				if err != nil {
					errs <- err
					return
				}

				versionMap := make(map[string][]directoryListing)

				for _, ver := range v.Versions {
					select {
					case <-ctx.Done():
						wg.Done()
						return
					default:
					}

					dir, err := x.getModuleVersionDirectoryListing(mod, ver)
					if err != nil {
						errs <- err
						return
					}

					dir = stripUselessEntries(dir)
					versionMap[ver] = dir
				}

				err = x.Queue.Put(Module{
					Name:     mod,
					Versions: versionMap,
				})
				if err != nil {
					errs <- err
				}
				wg.Done()
			}(mod, &wg)
		}
		wg.Wait()
		x.done <- true
		close(x.done)
	}()

	return errs
}

func (x *XQueuedCrawler) listAllModules() (chan string, error) {
	out := make(chan string, 100)

	u := url.URL{
		Scheme:   "https",
		Host:     API_HOST,
		Path:     "modules",
		RawQuery: "simple=1",
	}
	req, _ := http.NewRequest("GET", u.String(), nil)

	resp, err := x.DoRequest(req)
	if err != nil {
		return nil, errors.Errorf("failed to get simple list of modules: %s", err)
	}
	defer resp.Body.Close()

	var moduleList simpleModuleList
	body, err := ioutil.ReadAll(resp.Body)
	err = json.Unmarshal(body, &moduleList)

	if err != nil {
		return nil, errors.Errorf("failed to unmarshal response body: %s", err)
	}

	go func() {
		for _, mod := range moduleList {
			out <- mod
		}
	}()

	return out, nil
}

func (x *XQueuedCrawler) listModuleVersions(mod string) (versions, error) {
	u := url.URL{
		Scheme: "https",
		Host:   CDN_HOST,
		Path:   fmt.Sprintf("%s/meta/versions.json", mod),
	}
	req, _ := http.NewRequest("GET", u.String(), nil)

	resp, err := x.DoRequest(req)
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

func (x *XQueuedCrawler) getModuleVersionDirectoryListing(mod, version string) ([]directoryListing, error) {
	u := url.URL{
		Scheme: "https",
		Host:   CDN_HOST,
		Path:   fmt.Sprintf("%s/versions/%s/meta/meta.json", mod, version),
	}
	req, _ := http.NewRequest("GET", u.String(), nil)

	resp, err := x.DoRequest(req)
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
