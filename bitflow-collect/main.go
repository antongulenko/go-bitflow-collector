package main

import (
	"errors"
	"flag"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/antongulenko/go-bitflow-collector"
	"github.com/antongulenko/go-bitflow"
	"github.com/antongulenko/golib"
)

var (
	collect_local_interval = 500 * time.Millisecond
	sink_interval          = 500 * time.Millisecond
	collect_local          = false

	all_metrics           = false
	include_basic_metrics = false
	user_include_metrics  golib.StringSlice
	user_exclude_metrics  golib.StringSlice

	proc_collectors      golib.StringSlice
	proc_collector_regex golib.StringSlice
	proc_show_errors     = false
	proc_update_pids     = 1500 * time.Millisecond

	print_metrics = false
	libvirt_uri   = collector.LibvirtLocal() // collector.LibvirtSsh("host", "keyfile")
	ovsdb_host    = ""

	valueFactory = collector.ValueRingFactory{
		Interval: time.Second,
	}
)

var (
	includeMetricsRegexes []*regexp.Regexp
	excludeMetricsRegexes = []*regexp.Regexp{
		regexp.MustCompile("^mock$"),
		regexp.MustCompile("^net-proto/(UdpLite|IcmpMsg)"),                         // Some extended protocol-metrics
		regexp.MustCompile("^disk-io/...[0-9]"),                                    // Disk IO for specific partitions
		regexp.MustCompile("^disk-usage//.+/(used|free)$"),                         // All partitions except root
		regexp.MustCompile("^net-proto/tcp/(MaxConn|RtpAlgorithm|RtpMin|RtoMax)$"), // Some irrelevant TCP/IP settings
		regexp.MustCompile("^net-proto/ip/(DefaultTTL|Forwarding)$"),
	}
	includeBasicMetricsRegexes = []*regexp.Regexp{
		regexp.MustCompile("^(cpu|mem/percent)$"),
		regexp.MustCompile("^disk-io/.../(io|ioTime|ioBytes)$"),
		regexp.MustCompile("^net-io/(bytes|packets|dropped|errors)$"),
		regexp.MustCompile("^proc/.+/(cpu|mem/rss|disk/(io|ioBytes)|net-io/(bytes|packets|dropped|errors))$"),
	}
)

func do_main() int {
	flag.StringVar(&libvirt_uri, "libvirt", libvirt_uri, "Libvirt connection uri (default is local system)")
	flag.StringVar(&ovsdb_host, "ovsdb", ovsdb_host, "OVSDB host to connect to. Empty for localhost. Port is "+strconv.Itoa(collector.DefaultOvsdbPort))
	flag.BoolVar(&print_metrics, "metrics", print_metrics, "Print all available metrics and exit")
	flag.BoolVar(&all_metrics, "a", all_metrics, "Disable built-in filters on available metrics")
	flag.Var(&user_exclude_metrics, "exclude", "Metrics to exclude (only with -c, substring match)")
	flag.Var(&user_include_metrics, "include", "Metrics to include exclusively (only with -c, substring match)")
	flag.BoolVar(&include_basic_metrics, "basic", include_basic_metrics, "Include only a certain basic subset of metrics")

	flag.Var(&proc_collectors, "proc", "'key=substring' Processes to collect metrics for (substring match on entire command line)")
	flag.Var(&proc_collector_regex, "proc_regex", "'key=regex' Processes to collect metrics for (regex match on entire command line)")
	flag.BoolVar(&proc_show_errors, "proc_err", proc_show_errors, "Verbose: show errors encountered while getting process metrics")
	flag.DurationVar(&proc_update_pids, "proc_interval", proc_update_pids, "Interval for updating list of observed pids")

	flag.BoolVar(&collect_local, "c", collect_local, "Data source: collect local samples")
	flag.DurationVar(&collect_local_interval, "ci", collect_local_interval, "Interval for collecting local samples")
	flag.DurationVar(&sink_interval, "si", sink_interval, "Interval for sinking (sending/printing/...) data when collecting local samples")

	var p bitflow.CmdSamplePipeline
	p.ParseFlags()
	flag.Parse()
	defer golib.ProfileCpu()()

	// ====== Configure collectors
	valueFactory.Length = int(valueFactory.Interval/collect_local_interval) * 3 // Make sure enough samples can be buffered
	collector.RegisterMockCollector(&valueFactory)
	collector.RegisterPsutilCollectors(collect_local_interval*3/2, &valueFactory) // Update PIDs more often then metrics
	collector.RegisterLibvirtCollector(libvirt_uri, &valueFactory)
	collector.RegisterOvsdbCollector(ovsdb_host, &valueFactory)
	if len(proc_collectors) > 0 || len(proc_collector_regex) > 0 {
		regexes := make(map[string][]*regexp.Regexp)
		for _, substr := range proc_collectors {
			key, value := splitKeyValue(substr)
			regex := regexp.MustCompile(regexp.QuoteMeta(value))
			regexes[key] = append(regexes[key], regex)
		}
		for _, regexStr := range proc_collector_regex {
			key, value := splitKeyValue(regexStr)
			regex, err := regexp.Compile(value)
			golib.Checkerr(err)
			regexes[key] = append(regexes[key], regex)
		}
		for key, list := range regexes {
			collector.RegisterCollector(&collector.PsutilProcessCollector{
				CmdlineFilter:     list,
				GroupName:         key,
				PrintErrors:       proc_show_errors,
				PidUpdateInterval: proc_update_pids,
				Factory:           &valueFactory,
			})
		}
	}

	if all_metrics {
		excludeMetricsRegexes = nil
	}
	if include_basic_metrics {
		includeMetricsRegexes = append(includeMetricsRegexes, includeBasicMetricsRegexes...)
	}
	for _, exclude := range user_exclude_metrics {
		excludeMetricsRegexes = append(excludeMetricsRegexes,
			regexp.MustCompile(regexp.QuoteMeta(exclude)))
	}
	for _, include := range user_include_metrics {
		includeMetricsRegexes = append(includeMetricsRegexes,
			regexp.MustCompile(regexp.QuoteMeta(include)))
	}

	col := &collector.CollectorSource{
		CollectInterval: collect_local_interval,
		SinkInterval:    sink_interval,
		ExcludeMetrics:  excludeMetricsRegexes,
		IncludeMetrics:  includeMetricsRegexes,
	}
	if print_metrics {
		col.PrintMetrics()
		return 0
	}
	if collect_local {
		p.SetSource(col)
	}

	p.Init()
	return p.StartAndWait()
}

func main() {
	os.Exit(do_main())
}

func splitKeyValue(pair string) (string, string) {
	index := strings.Index(pair, "=")
	if index > 0 {
		return pair[:index], pair[index+1:]
	}
	golib.Checkerr(errors.New("-proc and -proc_regex must have argument format 'key=value'"))
	return "", ""
}