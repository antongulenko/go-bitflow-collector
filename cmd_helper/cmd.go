package cmd_helper

import (
	"flag"
	"log"
	"net/http"
	"sync"

	bitflow "github.com/antongulenko/go-bitflow"
	pipeline "github.com/antongulenko/go-bitflow-pipeline"
	"github.com/antongulenko/go-bitflow-pipeline/fork"
	"github.com/antongulenko/go-bitflow-pipeline/http_tags"
	"github.com/antongulenko/golib"
	"github.com/gorilla/mux"
)

const (
	RestApiPathPrefix = "/api"
	DefaultOutput     = "box://-"
)

type CmdDataCollector struct {
	Endpoints     *bitflow.EndpointFactory
	DefaultOutput string

	restApiEndpoint string
	fileOutputApi   FileOutputFilterApi
	outputs         golib.StringSlice
	flagTags        golib.KeyValueStringSlice
}

func (c *CmdDataCollector) ParseFlags() {
	flag.Var(&c.outputs, "o", "Data sink(s) for outputting data")
	flag.Var(&c.flagTags, "tag", "All collected samples will have the given tags (key=value) attached.")
	flag.StringVar(&c.restApiEndpoint, "api", "", "Enable REST API for controlling the collector. "+
		"The API can be used to control tags and enable/disable file output.")

	// Parse command line flags
	c.Endpoints = bitflow.NewEndpointFactory()
	c.Endpoints.RegisterGeneralFlagsTo(flag.CommandLine)
	c.Endpoints.RegisterOutputFlagsTo(flag.CommandLine)
	bitflow.RegisterGolibFlags()
	flag.Parse()
	golib.ConfigureLogging()
}

func (c *CmdDataCollector) MakePipeline() *pipeline.SamplePipeline {
	// Configure the data collector pipeline
	p := new(pipeline.SamplePipeline)
	if len(c.flagTags.Keys) > 0 {
		p.Add(pipeline.NewTaggingProcessor(c.flagTags.Map()))
	}
	if c.restApiEndpoint != "" {
		router := mux.NewRouter()
		tagger := http_tags.NewHttpTagger(RestApiPathPrefix, router)
		c.fileOutputApi.Register(RestApiPathPrefix, router)
		server := http.Server{
			Addr:    c.restApiEndpoint,
			Handler: router,
		}
		// Do not add this routine to any wait group, as it cannot be stopped
		go func() {
			tagger.Error(server.ListenAndServe())
		}()
		p.Add(tagger)
	}
	c.add_outputs(p)
	return p
}

func (c *CmdDataCollector) add_outputs(p *pipeline.SamplePipeline) {
	outputs := c.create_outputs()
	if len(outputs) == 1 {
		c.set_sink(p, outputs[0])
	} else {
		p.Sink = new(bitflow.EmptyMetricSink)

		// Create a multiplex-fork for all outputs
		num := len(outputs)
		builder := make(fork.MultiplexPipelineBuilder, num)
		for i, sink := range outputs {
			builder[i] = new(pipeline.SamplePipeline)
			c.set_sink(builder[i], sink)
		}
		p.Add(&fork.MetricFork{
			ParallelClose: true,
			Distributor:   fork.NewMultiplexDistributor(num),
			Builder:       builder,
		})
	}
}

func (c *CmdDataCollector) create_outputs() []bitflow.MetricSink {
	if len(c.outputs) == 0 && c.DefaultOutput != "" {
		c.outputs = []string{c.DefaultOutput}
	}
	var sinks []bitflow.MetricSink
	consoleOutputs := 0
	for _, output := range c.outputs {
		sink, err := c.Endpoints.CreateOutput(output)
		sinks = append(sinks, sink)
		golib.Checkerr(err)
		if bitflow.IsConsoleOutput(sink) {
			consoleOutputs++
		}
		if consoleOutputs > 1 {
			golib.Fatalln("Cannot define multiple outputs to stdout")
		}
	}
	return sinks
}

func (c *CmdDataCollector) set_sink(p *pipeline.SamplePipeline, sink bitflow.MetricSink) {
	p.Sink = sink

	// Add a filter to file outputs
	if _, isFile := sink.(*bitflow.FileSink); isFile {
		if c.restApiEndpoint != "" {
			p.Add(&pipeline.SampleFilter{
				Description: pipeline.String("Filter samples while no tags are defined via REST"),
				IncludeFilter: func(sample *bitflow.Sample, header *bitflow.Header) (bool, error) {
					return c.fileOutputApi.FileOutputEnabled, nil
				},
			})
		}
	}
}

type FileOutputFilterApi struct {
	lock              sync.Mutex
	FileOutputEnabled bool
}

func (api *FileOutputFilterApi) Register(pathPrefix string, router *mux.Router) {
	router.HandleFunc(pathPrefix+"/file_output", api.handleRequest).Methods("GET", "POST", "PUT", "DELETE")
}

func (api *FileOutputFilterApi) handleRequest(w http.ResponseWriter, r *http.Request) {
	api.lock.Lock()
	oldStatus := api.FileOutputEnabled
	newStatus := oldStatus
	switch r.Method {
	case "GET":
	case "POST", "PUT":
		newStatus = true
	case "DELETE":
		newStatus = false
	}
	api.FileOutputEnabled = newStatus
	api.lock.Unlock()

	var status string
	if api.FileOutputEnabled {
		status = "enabled"
	} else {
		status = "disabled"
	}
	status = "File output is " + status
	if oldStatus != newStatus {
		log.Println(status)
	}
	w.Write([]byte(status + "\n"))
}
