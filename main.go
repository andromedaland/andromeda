// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/dgo/v2"
	"github.com/dgraph-io/dgo/v2/protos/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wperron/depgraph/deno"
	"google.golang.org/grpc"
	"log"
	"net/http"
	"net/url"
	"os"
)

var client *dgo.Dgraph
var (
	filesInMap = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "crawler",
			Name: "files_in_map",
			Help: "total number of files tracked by the crawler",
		},
	)
)

type File struct {
	Uid       string   `json:"uid,omitempty"`
	Specifier string   `json:"specifier,omitempty"`
	DependsOn []File   `json:"depends_on,omitempty"`
	DType     []string `json:"dgraph.type,omitempty"`
}

type Module struct {
	Uid         string          `json:"uid,omitempty"`
	Name        string          `json:"name,omitempty"`
	Stars       int             `json:"stars,omitempty"`
	Description string          `json:"description,omitempty"`
	Version     []ModuleVersion `json:"version,omitempty"`
	DType       []string        `json:"dgraph.type,omitempty"`
}

type ModuleVersion struct {
	Uid           string `json:"uid,omitempty"`
	ModuleVersion string `json:"module_version,omitempty"`
	README        string `json:"README,omitempty"`
}

func init() {
	log.Println("start init.")

	log.Println("registering prometheus metrics")
	prometheus.MustRegister(filesInMap)

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

	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	))

	go http.ListenAndServe(":9093", nil)

	err := InitSchema(ctx)
	if err != nil {
		log.Fatalf("failed to initialize schema: %s\n", err)
	}
	log.Println("Successfully initialized schema on startup.")

	crawler := deno.NewDenoLandInstrumentedCrawler()
	modules, errs := crawler.IterateModules()
	inserted := InsertModules(ctx, modules)
	infos := IterateModuleInfo(inserted)
	done := InsertFiles(ctx, infos)

	go func() {
		for e := range errs {
			log.Printf("error: %s\n", e)
		}
	}()

	<-done
	log.Println("done.")
	os.Exit(0)
}

func InitSchema(ctx context.Context) error {
	// TODO(wperron) review schema, I don't like the current Module and
	//   ModuleVersion types, feels like theres a more 'graph-y' way to express
	//   these types.
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

// InsertModules is a passthrough function that makes sure the Module and
// ModuleVersion exist in the graph before inserting the Version's files.
func InsertModules(ctx context.Context, mods chan deno.Module) chan deno.Module {
	out := make(chan deno.Module)
	go func() {
		all := make(map[string]string)
		for mod := range mods {
			txn := client.NewTxn()
			uid := fmt.Sprintf("_:%s", mod.Name)
			if u, ok := all[mod.Name]; ok {
				uid = u
			}

			m := Module{
				Uid:       uid,
				Name: mod.Name,
				Stars: 0,
				DType:     []string{"Module"},
			}
			bytes, err := json.Marshal(m)
			if err != nil {
				log.Println(fmt.Errorf("failed to marshal module entry: %s", err))
			}

			mut := api.Mutation{}
			mut.SetJson = bytes
			resp, err := txn.Mutate(ctx, &mut)
			if err != nil {
				log.Println(fmt.Errorf("failed to run mutation for file %s: %s", mod.Name, err))
			}

			all = merge(all, resp.Uids)
			out <- mod
		}
		close(out)
	}()

	return out
}

func InsertFiles(ctx context.Context, mods chan deno.DenoInfo) chan bool {
	done := make(chan bool)
	go func() {
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
				filesInMap.Set(float64(len(all)))
				count++
			}

			err := txn.Commit(ctx)
			if err != nil {
				log.Fatalf("failed to commit transaction: %s\n", err)
			}
			log.Printf("transaction completed, %d mutations completed\n", count)
		}

		log.Println("finished inserting all files")
		done <- true
		close(done)
	}()

	return done
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
