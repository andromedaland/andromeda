// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wperron/depgraph/constellation"
	"github.com/wperron/depgraph/deno"
)

var specifierDenoInfoHist prometheus.Histogram
var moduleDenoInfoHist prometheus.Histogram

func init() {
	specifierDenoInfoHist = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "deno_info_specifier_hist",
			Help: "A histogram for the duration of `deno info` for a single specifier",
		},
	)

	moduleDenoInfoHist = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "deno_info_module_hist",
			Help: "A histogram for the duration of `deno info` for an entire module version",
		},
	)

	prometheus.MustRegister(specifierDenoInfoHist, moduleDenoInfoHist)
}

func main() {
	log.Println("start.")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sig := make(chan os.Signal)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
		s := <-sig
		log.Printf("Received signal %s, cancelling context\n", s)
		cancel()
	}()

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

	if ok := deno.Exists(); !ok {
		log.Fatal("stopping: executable `deno` not found in PATH")
	}

	// AWS config
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		log.Fatal(err)
	}

	q := deno.NewSQSQueue(cfg, "https://sqs.us-east-1.amazonaws.com/831183038069/andromeda-test-1", 0)
	crawler := deno.NewXQueuedCrawler(q)

	toInsert, errs := crawler.IterateModules(ctx)
	crawlErrs := WatchQueue(ctx, crawler, q)

	inserted := constellation.InsertModules(ctx, toInsert)
	infos := IterateModuleInfo(ctx, inserted, q)
	done := constellation.InsertFiles(ctx, infos)

	merged := mergeErrors(errs, crawlErrs)
	go func() {
		for e := range merged {
			log.Printf("error: %s\n", e)
		}
	}()

	<-done
	log.Println("done.")
	os.Exit(0)
}

// WatchQueue is an infinite loop that checks the number of messages present in
// an SQSQueue instance and triggers the Crawler when it gets below a certain
// threshold
func WatchQueue(ctx context.Context, crawler *deno.XQueuedCrawler, sq *deno.SQSQueue) chan error {
	errs := make(chan error)

	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Println("received cancel signal, closing WatchQueue")
				close(errs)
			default:
			}

			num, err := sq.Approx()
			if err != nil {
				errs <- err
				continue
			}

			if num < 50 {
				crawlErrs := crawler.Crawl(ctx)
				go func() {
					for e := range crawlErrs {
						errs <- e
					}
				}()
				<-crawler.Done()
			}

			// TODO(wperron) find something better than sleep (timer maybe?)
			time.Sleep(1 * time.Second)
		}
	}()

	return errs
}

// IterateModuleInfo consumes the channel of Module and runs deno.ExecInfo for
// every source code file of every version
// TODO(wperron): refactor logic specific to deno.land/x to deno/x.go
func IterateModuleInfo(ctx context.Context, mods chan deno.Module, sq *deno.SQSQueue) chan deno.DenoInfo {
	out := make(chan deno.DenoInfo)
	go func() {
		for mod := range mods {
			modStart := time.Now()
			for v, entrypoints := range mod.Versions {
				for _, file := range entrypoints {
					select {
					case <-ctx.Done():
						// simply exit as soon as the context is cancelled, as a
						// side effect the module message doesn't get removed
						// from the queue. This means the whole module will get
						// picked up and started from the beginning on the next
						// run, which is a non issue since the process is
						// idempotent anyway
						log.Println("received cancel signal, closing IterateModuleInfo")
						close(out)
						return
					default:
					}

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

					specificerStart := time.Now()
					info, err := deno.ExecInfo(ctx, u)
					specifierDenoInfoHist.Observe(time.Since(specificerStart).Seconds())

					if err != nil {
						log.Println(fmt.Errorf("failed to run deno exec on path %s: %s", u.String(), err))
						// TODO(wperron) find a way to represent broken dependencies in tree
						continue
					}
					out <- info
				}
			}
			if err := sq.Delete(mod); err != nil {
				log.Fatalf("failed to delete %s: %s", mod.Name, err)
			}
			moduleDenoInfoHist.Observe(time.Since(modStart).Seconds())
		}
		close(out)
	}()
	return out
}

func mergeErrors(chans ...chan error) chan error {
	out := make(chan error)

	for _, c := range chans {
		go func(c <-chan error) {
			for v := range c {
				out <- v
			}
		}(c)
	}
	return out
}
