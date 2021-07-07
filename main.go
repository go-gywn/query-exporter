package main

import (
	"flag"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/ghodss/yaml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	log "github.com/sirupsen/logrus"
)

var address string
var group map[string]Instances
var collectors map[string]*Collector

const (
	defaultQueryTimeout   = 1
	defaultThreadCount    = 32
	defaultAddress        = "0.0.0.0:9104"
	defaultConfigDatabase = "config-database.yml"
	defaultConfigMetrics  = "config-metrics.yml"
)

func main() {
	var err error
	var b []byte

	var threads int64
	var cfg1, cfg2 string
	flag.Int64Var(&threads, "threads", defaultThreadCount, "collector thread count")
	flag.StringVar(&address, "address", defaultAddress, "http server port")
	flag.StringVar(&cfg1, "config-database", defaultConfigDatabase, "configuration databases")
	flag.StringVar(&cfg2, "config-metrics", defaultConfigMetrics, "configuration metrics")
	flag.Parse()

	// ===========================
	log.Debugf("[address] %s", address)
	log.Debugf("[threads] %d", threads)
	log.Debugf("[config-database] %s", cfg1)
	log.Debugf("[config-metrics] %s", cfg2)

	// ===========================
	// Load target database config
	// ===========================
	if b, err = ioutil.ReadFile(cfg1); err != nil {
		log.Fatalf("Failed to read database config file: %s", err)
	}

	if err := yaml.Unmarshal(b, &group); err != nil {
		log.Fatalf("Failed to load database config: %s", err)
	}
	log.Debugf("[config-database] %s", group)

	// ===========================
	// Load target metric config
	// ===========================
	if b, err = ioutil.ReadFile(cfg2); err != nil {
		log.Fatalf("Failed to read metric config file: %s", err)
	}

	if err := yaml.Unmarshal(b, &collectors); err != nil {
		log.Fatalf("Failed to load metric config: %s", err)
	}
	log.Debugf("[config-metrics] %s", collectors)

	// ===========================
	// Make statusDesc for collector result
	// ===========================
	statusDesc := prometheus.NewDesc(
		prometheus.BuildFQName(namespace, exporter, "status"),
		"Query collect status",
		[]string{"instance"}, nil,
	)
	log.Debugf("[statusDesc] %s", statusDesc)

	// ===========================
	// Regist collector and start exporter
	// ===========================
	prometheus.MustRegister(version.NewCollector(namespace + "_" + exporter))
	for path, collector := range collectors {

		// Initialize metricDesc for collector
		log.Debugf("[path] %s, [collector]", path, collector)
		for i := range collector.Collects {
			collect := &collector.Collects[i]
			for metricKey, metric := range collect.Metrics {
				metric.Labels = append(metric.Labels, "instance")
				metric.metricDesc = prometheus.NewDesc(
					prometheus.BuildFQName(namespace, exporter, metricKey),
					metric.Description,
					metric.Labels, nil,
				)
				log.Debug(">> ", metric)
			}
			if collect.Timeout <= 0 {
				collect.Timeout = defaultQueryTimeout
			}
		}

		// Make slice for collector thread
		slots := make([]Instances, threads)
		for i := range slots {
			slots[i] = Instances{}
		}

		// Split for each collector thread
		i := 0
		for _, target := range collector.Targets {
			for k, v := range group[target] {
				v.Name = k
				slots[i%len(slots)][v.Name] = v
				i += 1
			}
		}

		// Regist collector
		registry := prometheus.NewRegistry()
		for i := range slots {
			log.Debugf("[thread_%d] %d, [detail] %s", i, len(slots[i]), slots[i])
			registry.Register(&QueryCollector{instances: slots[i], collects: collector.Collects, StatusDesc: statusDesc})
		}

		// Regist http handler
		log.Infof("Regist handler %s/%s", address, path)
		http.HandleFunc("/"+path, func(w http.ResponseWriter, r *http.Request) {
			h := promhttp.HandlerFor(prometheus.Gatherers{
				prometheus.DefaultGatherer,
				registry,
			}, promhttp.HandlerOpts{})
			h.ServeHTTP(w, r)
		})

	}

	// ===========================
	// start server
	// ===========================
	log.Infof("Starting http server - %s", address)
	if err = http.ListenAndServe(address, nil); err != nil {
		log.Fatalf("Failed to start http server: %s", err)
	}
}

func init() {
	// Version
	version.Version = "0.1"
	version.Branch = "main"

	// Log level
	level := log.InfoLevel
	if lvl, err := log.ParseLevel(os.Getenv("LOG_LEVEL")); err == nil {
		level = lvl
	}
	log.Infof("Log level>> %s [error|info|debug]", level)
	log.SetLevel(level)
}

// Instances target instance map
type Instances map[string]*Instance

// Instance target instance
type Instance struct {
	Name string
	Type string
	DSN  string
}

// Collector metric groups
type Collector struct {
	Targets  []string
	Collects []Collect
}

// Collect collect structure
type Collect struct {
	Query   string
	Timeout int
	Metrics Metrics
}

// Metrics metric map
type Metrics map[string]*Metric

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
