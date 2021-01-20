// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package constellation

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/dgo/v2"
	"github.com/dgraph-io/dgo/v2/protos/api"
	"github.com/wperron/depgraph/deno"
	"google.golang.org/grpc"
	"log"
	"strings"
)

var client *dgo.Dgraph

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
		for mod := range mods {
			txn := client.NewTxn()
			defer func(ctx context.Context, t *dgo.Txn) {
				err := t.Discard(ctx)
				if err != nil {
					log.Println(fmt.Errorf("failed to discard txn: %s", err))
				}
			}(ctx, txn)

			// map of specifier->uid that were created during the processing of
			// this module.
			uids := make(map[string]string)

			for k, f := range mod.Files {
				res, err := mutateFile(ctx, txn, k, f)
				if err != nil {
					log.Fatalf("failed to run mutation for %s: %s\n", k, err)
				}

				uids = merge(uids, res)
			}

			err := txn.Commit(ctx)
			if err != nil {
				log.Fatalf("failed to commit transaction: %s\n", err)
			}
			log.Printf("transaction completed for %s\n", mod.Module)

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

			item, err := GetSpecifierUid(d)
			if err != nil {
				log.Fatalf("failed to get specificer %s from DynamoDB: %s\n", d, err)
			}

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
	item, err := GetSpecifierUid(specifier)
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
	resp, err := txn.Mutate(ctx, &mut)
	if err != nil {
		log.Println(fmt.Errorf("failed to run mutation for file %s: %s", specifier, err))
	}

	// the returned blanks in the Uids map only contain the right hand part of
	// the blank that was used in the mutation (_:<specifier>). For all intents
	// and purposes, the resp.Uids map is a specifier->uids map.
	return resp.Uids, nil
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
