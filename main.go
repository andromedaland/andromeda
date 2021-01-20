// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package main

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wperron/depgraph/constellation"
	"github.com/wperron/depgraph/deno"
	"log"
	"net/http"
	"net/url"
	"os"
)

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

	err := constellation.InitSchema(ctx)
	if err != nil {
		log.Fatalf("failed to initialize schema: %s\n", err)
	}
	log.Println("Successfully initialized schema on startup.")

	crawler := deno.NewDenoLandInstrumentedCrawler()
	modules, errs := crawler.IterateModules()
	inserted := constellation.InsertModules(ctx, modules)
	infos := IterateModuleInfo(inserted)
	done := constellation.InsertFiles(ctx, infos)

	go func() {
		for e := range errs {
			log.Printf("error: %s\n", e)
		}
	}()

	<-done
	log.Println("done.")
	os.Exit(0)
}

// TODO(wperron): refactor logic specific to deno.land/x to deno/x.go
func IterateModuleInfo(mods chan deno.Module) chan deno.DenoInfo {
	out := make(chan deno.DenoInfo)
	go func() {
		for mod := range mods {
			for v, entrypoints := range mod.Versions {
				for _, file := range entrypoints {
					var path string
					if mod.Name == "std" {
						path = fmt.Sprintf("%s@%s%s", mod.Name, v, file.Path)
					} else {
						path = fmt.Sprintf("x/%s@%s%s", mod.Name, v, file.Path)
					}

					u := url.URL{
						Scheme: "https",
						Host:   "deno.land",
						Path:   path,
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
