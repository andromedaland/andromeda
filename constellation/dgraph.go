// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package constellation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dgraph-io/dgo/v2"
	"github.com/dgraph-io/dgo/v2/protos/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/wperron/depgraph/deno"
	"google.golang.org/grpc"
)

var client *dgo.Dgraph
var trxCounter prometheus.Counter
var mutationsCounter prometheus.Counter
var commitLatency prometheus.Histogram

func init() {
	trxCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "transactions_total",
			Help: "A counter for transactions in DGraph",
		},
	)

	mutationsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mutations_total",
			Help: "A counter for mutations in DGraph",
		},
	)

	commitLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "commit_latency",
			Help: "A histogram of transaction latencies",
		},
	)

	prometheus.MustRegister(trxCounter, mutationsCounter, commitLatency)
}

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
	// TODO(wperron): parameterize alpha URL
	log.Println("connecting to the dgraph cluster")
	d, err := grpc.Dial("localhost:9080", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed to dial the alpha server at localhost:9080: %s\n", err)
	}

	client = dgo.NewDgraphClient(api.NewDgraphClient(d))

	// Drop all data including schema from the dgraph instance. Useful for PoC
	//log.Println("dropping existing data in the dgraph cluster")
	//err = client.Alter(context.Background(), &api.Operation{DropOp: api.Operation_ALL})
	//if err != nil {
	//	log.Fatalf("error while cleaning the dgraph instance: %s\n", err)
	//}
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
			select {
			case <-ctx.Done():
				log.Println("received cancel signal, closing InsertModules")
				return
			default:
			}

			trxCounter.Add(1)

			txn := client.NewTxn()
			uid := fmt.Sprintf("_:%s", mod.Name)
			if u, ok := all[mod.Name]; ok {
				uid = u
			}

			m := Module{
				Uid:   uid,
				Name:  mod.Name,
				Stars: 0,
				DType: []string{"Module"},
			}
			bytes, err := json.Marshal(m)
			if err != nil {
				log.Println(fmt.Errorf("failed to marshal module entry: %s", err))
				discard(ctx, txn)
				continue
			}

			mut := api.Mutation{}
			mut.SetJson = bytes
			mutationsCounter.Add(1)
			resp, err := txn.Mutate(ctx, &mut)
			if err != nil {
				log.Println(fmt.Errorf("failed to run mutation for file %s: %s", mod.Name, err))
				discard(ctx, txn)
				continue
			}

			start := time.Now()
			err = txn.Commit(ctx)
			commitLatency.Observe(time.Since(start).Seconds())
			if err != nil {
				log.Fatalf("failed to commit transaction: %s\n", err)
				discard(ctx, txn)
				continue
			}

			all = merge(all, resp.Uids)
			out <- mod
		}
		close(out)
	}()

	return out
}

// InsertFiles iterates over a channel of DenoInfo and inserts every specifier
// in it in the DGraph cluster
func InsertFiles(ctx context.Context, mods chan deno.DenoInfo) chan bool {
	done := make(chan bool)
	go func() {
		for mod := range mods {
			trxCounter.Add(1)

			txn := client.NewTxn()

		inner:
			for k, f := range mod.Files {
				select {
				case <-ctx.Done():
					log.Println("received cancel signal, closing InsertFiles")
					break inner
				default:
				}

				uids, err := mutateFile(ctx, txn, k, f)
				if err != nil {
					log.Fatalf("failed to run mutation for %s: %s\n", k, err)
					discard(ctx, txn)
					continue
				}

				for specifier, uid := range uids {
					// TODO(wperron): there's probably a better to filter for only
					//   the UIDs that were created as part of this mutation
					if strings.HasPrefix(specifier, "https://") {
						if err := PutEntry(Item{
							Specifier: specifier,
							Uid:       uid,
						}); err != nil {
							log.Fatal(fmt.Errorf("\tfailed to put entry for %s: %s", specifier, err))
						}
					}
				}
			}

			start := time.Now()
			err := txn.Commit(ctx)
			commitLatency.Observe(time.Since(start).Seconds())
			if err != nil {
				log.Printf("failed to commit transaction: %s\n", err)
				discard(ctx, txn)
				continue
			}
			log.Printf("transaction completed for %s\n", mod.Module)
		}

		log.Println("finished inserting all files")
		done <- true
		close(done)
	}()

	return done
}

func mutateFile(ctx context.Context, txn *dgo.Txn, specifier string, entry deno.FileEntry) (map[string]string, error) {
	deps := make([]File, len(entry.Deps))
	// map specifier->blank uid
	// used later to insert into DynamoDB UIDs that were created in
	// this mutation
	blanks := make(map[string]string)
	if len(entry.Deps) > 0 {
		for _, d := range entry.Deps {
			uid := fmt.Sprintf("_:%s", d)

			item, err := GetEntry(d)
			if err != nil {
				log.Fatalf("failed to get specificer %s from DynamoDB: %s\n", d, err)
			}

			// Uid is a projected attribute of the item in DDB. functionnaly, there
			// is no difference between checking for `Uid == ""` than checking for
			// `Specificer == ""`. In this case, checking for Uid is simply the
			// closest to the semantics of "check if item is in graph."
			if item.Uid != "" {
				uid = item.Uid
			} else {
				// keep track of blank UIDs used in the mutation
				blanks[d] = uid
			}
			deps = append(deps, File{Uid: uid})
		}
	}

	uid := fmt.Sprintf("_:%s", specifier)
	item, err := GetEntry(specifier)
	if err != nil {
		log.Fatal(err)
	}

	if item.Uid != "" {
		uid = item.Uid
	} else {
		// The item doesn't exist in DynamoDB or in DGraph yet
		blanks[specifier] = uid
	}

	file := File{
		Uid:       uid,
		Specifier: specifier,
		DependsOn: deps,
		DType:     []string{"File"},
	}
	bytes, err := json.Marshal(file)
	if err != nil {
		log.Println(fmt.Errorf("failed to marshal file entry: %s", err))
	}

	mut := api.Mutation{}
	mut.SetJson = bytes
	mutationsCounter.Add(1)
	resp, err := txn.Mutate(ctx, &mut)
	if err != nil {
		e := fmt.Errorf("failed to run mutation for file %s: %s", specifier, err)
		log.Println(e)
		return map[string]string{}, nil
	}

	// the returned blanks in the Uids map only contain the right hand part of
	// the blank that was used in the mutation (_:<specifier>). For all intents
	// and purposes, the resp.Uids map is a specifier->uids map.
	return resp.Uids, nil
}

func discard(ctx context.Context, txn *dgo.Txn) {
	select {
	case <-ctx.Done():
		log.Println("context is already cancelled, exiting early")
		return
	default:
	}
	err := txn.Discard(ctx)
	if err != nil {
		log.Println(fmt.Errorf("failed to discard txn: %s", err))
	}
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
