// Copyright 2020 William Perron. All rights reserved. MIT License.
package main

import (
	"context"
	"fmt"
	"github.com/dgraph-io/dgo"
	"github.com/dgraph-io/dgo/protos/api"
	"github.com/wperron/depgraph/pkg/denoapi"
	"github.com/wperron/depgraph/pkg/denoinfo"
	"google.golang.org/grpc"
	"log"
	"net/url"
	"os"
	"sync"
)

var client *dgo.Dgraph

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

	denoClient := denoapi.NewClient()
	modules, errs := denoClient.IterateModules()

	wg := sync.WaitGroup{}
	wg.Add(2)
	go func(wg *sync.WaitGroup) {
		for mod := range modules {
			for v, entrypoints := range mod.Versions {
				for _, file := range entrypoints {
					u := url.URL{
						Scheme: "https",
						Host: "deno.land",
						Path: fmt.Sprintf("x/%s@%s%s", mod.Name, v, file.Path),
					}
					info, err := denoinfo.ExecInfo(u)
					if err != nil {
						log.Println(fmt.Errorf("failed to run deno exec on path %s: %s", u.String(), err))
						// TODO(wperron) find a way to represent broken dependencies in tree
						continue
					}
					log.Println(info)
				}
			}
		}
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
				dependent_of
			}
			name: string @index(term, fulltext, trigram) .
			description: string @index(term, fulltext, trigram) .
			stars: int .
			version: [uid] @reverse .
			module_version: string @index(term, fulltext, trigram) .
			README: string @index(term, fulltext, trigram) .
			file_specifier: [uid] .
			specifier: string .
			depends_on: [uid] @reverse .
			dependent_of: [uid] @reverse .
		`,
	})
}
