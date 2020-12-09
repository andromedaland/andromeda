// Copyright 2020 William Perron. All rights reserved. MIT License.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"sync"

	"github.com/dgraph-io/dgo/v2"
	"github.com/dgraph-io/dgo/v2/protos/api"
	"github.com/wperron/depgraph/deno"
	"google.golang.org/grpc"
)

var client *dgo.Dgraph

type File struct {
	Uid       string   `json:"uid,omitempty"`
	Specifier string   `json:"specifier,omitempty"`
	DependsOn []File   `json:"depends_on,omitempty"`
	DType     []string `json:"dgraph.type,omitempty"`
}

func init() {
	log.Println("start init.")
	// TODO(wperron): parameterize alpha URL
	log.Println("connecting to the dgraph cluster")
	d, err := grpc.Dial("localhost:9080", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed to dial the alpha server at localhost:9080: %s\n", err)
	}

	client = dgo.NewDgraphClient(api.NewDgraphClient(d))

	// Drop all data including schema from the dgraph instance. Useful for PoC
	log.Println("dropping existing data in the dgraph cluster")
	err = client.Alter(context.Background(), &api.Operation{DropOp: api.Operation_ALL})
	if err != nil {
		log.Fatalf("error while cleaning the dgraph instance: %s\n", err)
	}
	log.Println("end init.")
}

func main() {
	log.Println("start.")
	ctx := context.Background()

	err := InitSchema(ctx)
	if err != nil {
		log.Fatalf("failed to initialize schema: %s\n", err)
	}

	log.Println("Successfully initialized schema on startup.")

	denoClient := deno.NewClient()
	modules, errs := denoClient.IterateModules()

	wg := sync.WaitGroup{}
	wg.Add(2)
	go func(wg *sync.WaitGroup) {
		infos := IterateModuleInfo(modules)
		wg.Add(1)
		go InsertModules(ctx, wg, infos)
		wg.Done()
	}(&wg)

	go func(wg *sync.WaitGroup) {
		for err := range errs {
			log.Println(fmt.Errorf("error while consuming modules: %s", err))
		}
		wg.Done()
	}(&wg)

	wg.Wait()
	log.Println("done.")
	os.Exit(0)
}

func InitSchema(ctx context.Context) error {
	return client.Alter(ctx, &api.Operation{
		Schema: `
			type Module {
				name
				description
				stars
				version
			}
			type ModuleVersion {
				module_version
				README
				file_specifier
			}
			type File {
				specifier
				depends_on
			}
			name: string @index(term, fulltext, trigram) .
			description: string @index(term, fulltext, trigram) .
			stars: int .
			version: [uid] @reverse .
			module_version: string @index(term, fulltext, trigram) .
			README: string @index(term, fulltext, trigram) .
			file_specifier: [uid] .
			specifier: string @index(term, fulltext, trigram) .
			depends_on: [uid] @reverse .
		`,
	})
}

func InsertModules(ctx context.Context, wg *sync.WaitGroup, mods chan deno.DenoInfo) {
	count := 0
	all := make(map[string]string)
	for mod := range mods {
		txn := client.NewTxn()
		defer txn.Discard(ctx)
		for k, f := range mod.Files {
			// guard clause, exit early
			if len(all) > 1000000 {
				log.Println("guard clause reached, exiting early")
				_ = txn.Commit(ctx)
				wg.Done()
				return
			}

			deps := make([]File, len(f.Deps))
			for _, d := range f.Deps {
				uid := fmt.Sprintf("_:%s", d)
				if u, ok := all[d]; ok {
					uid = u
				}
				deps = append(deps, File{Uid: uid})
			}

			uid := fmt.Sprintf("_:%s", k)
			if u, ok := all[k]; ok {
				uid = u
			}

			file := File{
				Uid:       uid,
				Specifier: k,
				DependsOn: deps,
				DType:     []string{"File"},
			}
			bytes, err := json.Marshal(file)
			if err != nil {
				log.Println(fmt.Errorf("failed to marshal file entry: %s", err))
			}

			mut := api.Mutation{}
			mut.SetJson = bytes
			resp, err := txn.Mutate(ctx, &mut)
			if err != nil {
				log.Println(fmt.Errorf("failed to run mutation for file %s: %s", k, err))
			}

			all = merge(all, resp.Uids)
			count++
		}

		err := txn.Commit(ctx)
		if err != nil {
			log.Fatalf("failed to commit transaction: %s\n", err)
		}
		log.Printf("transaction completed, %d mutations completed\n", count)
	}

	wg.Done()
}

func IterateModuleInfo(mods chan deno.Module) chan deno.DenoInfo {
	out := make(chan deno.DenoInfo)
	go func() {
		for mod := range mods {
			for v, entrypoints := range mod.Versions {
				for _, file := range entrypoints {
					u := url.URL{
						Scheme: "https",
						Host:   "deno.land",
						Path:   fmt.Sprintf("x/%s@%s%s", mod.Name, v, file.Path),
					}
					info, err := deno.ExecInfo(u)
					if err != nil {
						log.Println(fmt.Errorf("failed to run deno exec on path %s: %s", u.String(), err))
						// TODO(wperron) find a way to represent broken dependencies in tree
						continue
					}
					out <- info
				}
			}
		}
		close(out)
	}()
	return out
}

func merge(maps ...map[string]string) (out map[string]string) {
	out = make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			if _, ok := out[k]; !ok {
				out[k] = v
			}
		}
	}
	return
}
