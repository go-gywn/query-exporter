package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/ghodss/yaml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
)

func main() {
	var err error
	var config Config

	// =====================
	// Get OS parameter
	// =====================
	var configFile string
	flag.StringVar(&configFile, "config", "config.yml", "configuration file")
	flag.Parse()

	// =====================
	// Load config
	// =====================
	var b []byte
	if b, err = ioutil.ReadFile(configFile); err != nil {
		log.Errorf("Failed to read config file: %s", err)
		os.Exit(1)
	}

	// =====================
	// Load yaml
	// =====================
	if err := yaml.Unmarshal(b, &config); err != nil {
		log.Errorf("Failed to load config: %s", err)
		os.Exit(1)
	}

	// ========================
	// Regist describe
	// ========================
	prometheus.MustRegister(version.NewCollector("query_exporter"))
	for metricName, metric := range config.Metrics {
		metric.Labels = append(metric.Labels, "instance")
		metric.metricDesc = prometheus.NewDesc(
			prometheus.BuildFQName("", config.Type, metricName),
			metric.Description,
			metric.Labels, nil,
		)
		config.Metrics[metricName] = metric
		log.Infof("metric description for \"%s\" registerd", metricName)
	}

	// ========================
	// Regist handler
	// ========================
	registry := prometheus.NewRegistry()
	registry.MustRegister(&QueryExporter{
		cfg: config,
	})
	gatherers := prometheus.Gatherers{
		prometheus.DefaultGatherer,
		registry,
	}

	// http handler
	h := promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{})

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		// Delegate http serving to Prometheus client library,
		//  which will call collector.Collect.
		h.ServeHTTP(w, r)
	})

	// start server
	log.Infof("Starting http server - %s", config.Bind)
	if err := http.ListenAndServe(config.Bind, nil); err != nil {
		log.Errorf("Failed to start http server: %s", err)
	}
}

// Config config structure
type Config struct {
	Bind     string
	Instance string
	Type     string
	DSN      string
	User     string
	Pass     string
	Metrics  Metrics
}

// Metrics metric map
type Metrics map[string]Metric

// Metric metric structure
type Metric struct {
	Query       string
	Type        string
	Description string
	Labels      []string
	Value       string
	metricDesc  *prometheus.Desc
}

// QueryExporter exporter
type QueryExporter struct {
	cfg Config
}

// Describe prometheus describe
func (e *QueryExporter) Describe(ch chan<- *prometheus.Desc) {
}

// Collect prometheus collect
func (e *QueryExporter) Collect(ch chan<- prometheus.Metric) {
	// ========================
	// Connect to database
	// ========================
	conInfo := fmt.Sprintf("%s:%s@%s", e.cfg.User, e.cfg.Pass, e.cfg.DSN)
	db, err := sql.Open(e.cfg.Type, conInfo)
	if err != nil {
		log.Errorf("Connect to %s database failed: %s", e.cfg.Type, err)
		return
	}
	defer db.Close()

	for _, metric := range e.cfg.Metrics {
		rows, err := db.Query(metric.Query)
		if err != nil {
			log.Errorf("Failed to execute query: %s", err)
			continue
		}

		cols, err := rows.Columns()
		des := make([]interface{}, len(cols))
		res := make([][]byte, len(cols))
		for i := range cols {
			des[i] = &res[i]
		}

		// fetch database
		for rows.Next() {
			err = rows.Scan(des...)
			data := make(map[string]string)
			for i, bytes := range res {
				data[cols[i]] = string(bytes)
			}

			// upsert instance
			if data["instance"] == "" {
				data["instance"] = e.cfg.Instance
			}

			// Metric labels
			labelVals := []string{}
			for _, label := range metric.Labels {
				labelVals = append(labelVals, data[label])
			}

			// Metric value
			val, _ := strconv.ParseFloat(data[metric.Value], 64)

			// Add metric
			switch strings.ToLower(metric.Type) {
			case "counter":
				ch <- prometheus.MustNewConstMetric(metric.metricDesc, prometheus.CounterValue, val, labelVals...)
			case "guage":
				ch <- prometheus.MustNewConstMetric(metric.metricDesc, prometheus.GaugeValue, val, labelVals...)
			default:
				continue
			}
		}
	}
}
