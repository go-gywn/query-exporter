package main

import (
	"flag"
	"io/ioutil"
	"net/http"

	"github.com/ghodss/yaml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
)

var address string
var group map[string]Instances
var collectors map[string]Collector

func main() {
	var err error
	var b []byte

	var cfg1, cfg2 string
	// var workers int
	// flag.IntVar(&workers, "workers", 8, "worker count")
	flag.StringVar(&address, "address", "0.0.0.0:9104", "http server port")
	flag.StringVar(&cfg1, "config-database", "config-database.yml", "configuration databases")
	flag.StringVar(&cfg2, "config-metrics", "config-metrics.yml", "configuration metrics")
	flag.Parse()

	// ===========================
	// Load target database config
	// ===========================
	if b, err = ioutil.ReadFile(cfg1); err != nil {
		log.Fatalf("Failed to read database config file: %s", err)
	}

	if err := yaml.Unmarshal(b, &group); err != nil {
		log.Fatalf("Failed to load database config: %s", err)
	}

	// ===========================
	// Load target metric config
	// ===========================
	if b, err = ioutil.ReadFile(cfg2); err != nil {
		log.Fatalf("Failed to read metric config file: %s", err)
	}

	if err := yaml.Unmarshal(b, &collectors); err != nil {
		log.Fatalf("Failed to load metric config: %s", err)
	}

	for _, instances := range group {
		for k, v := range instances {
			v.Instance = k
			instances[k] = v
		}
	}

	// ===========================
	// Regist metric describe
	// ===========================
	prometheus.MustRegister(version.NewCollector(namespace + "_" + exporter))
	for collectorKey, collector := range collectors {
		for _, collect := range collector.Collects {
			for metricKey, metric := range collect.Metrics {
				metric.Labels = append(metric.Labels, "instance")
				metric.metricDesc = prometheus.NewDesc(
					prometheus.BuildFQName(namespace, exporter, metricKey),
					metric.Description,
					metric.Labels, nil,
				)
				collect.Metrics[metricKey] = metric
				log.Debug(">> ", metric)
			}
		}

		// config http handler
		log.Infof("Regist handler %s/%s", address, collectorKey)
		http.HandleFunc("/"+collectorKey, GetHandlerFor(collector))
		collectors[collectorKey] = collector
	}

	// ===========================
	// start server
	// ===========================
	log.Infof("Starting http server - %s", address)
	if err = http.ListenAndServe(address, nil); err != nil {
		log.Fatalf("Failed to start http server: %s", err)
	}
}

// GetHandlerFor new metric handler
func GetHandlerFor(collector Collector) http.HandlerFunc {
	registry := prometheus.NewRegistry()

	// regist query exporter
	registry.MustRegister(&QueryExporter{
		collector: collector,
	})

	gatherers := prometheus.Gatherers{
		prometheus.DefaultGatherer,
		registry,
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Delegate http serving to Prometheus client library, which will call collector.Collect.
		h := promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{})
		h.ServeHTTP(w, r)
	}
}

// Instances target instance map
type Instances map[string]Instance

// Instance target instance
type Instance struct {
	Instance string
	Type     string
	DSN      string
	User     string
	Pass     string
}

// Collector metric groups
type Collector struct {
	Targets  []string
	Collects []Collect
}

// Collect collect structure
type Collect struct {
	Query   string
	Metrics Metrics
}

// Metrics metric map
type Metrics map[string]Metric

// Metric metric map
type Metric struct {
	Name        string
	Type        string
	Description string
	Labels      []string
	Value       string
	Query       string
	metricDesc  *prometheus.Desc
}
