package main

import (
	"database/sql"
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/ghodss/yaml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	log "github.com/sirupsen/logrus"
)

const (
	name = "query_exporter"
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
	// Load config & yaml
	// =====================
	var b []byte
	if b, err = ioutil.ReadFile(configFile); err != nil {
		log.Errorf("Failed to read config file: %s", err)
		os.Exit(1)
	}

	// Load yaml
	if err := yaml.Unmarshal(b, &config); err != nil {
		log.Errorf("Failed to load config: %s", err)
		os.Exit(1)
	}

	// ========================
	// Regist describe
	// ========================
	for metricName, metric := range config.Metrics {
		metric.Labels = append(metric.Labels, "instance")
		metric.metricDesc = prometheus.NewDesc(
			prometheus.BuildFQName(name, config.Type, metricName),
			metric.Description,
			metric.Labels, nil,
		)
		config.Metrics[metricName] = metric
		log.Infof("metric description for \"%s\" registerd", metricName)
	}

	// ========================
	// Regist handler
	// ========================
	prometheus.MustRegister(&QueryCollector{
		cfg: &config,
	}, version.NewCollector(name))

	// http handler
	h := promhttp.HandlerFor(prometheus.Gatherers{
		prometheus.DefaultGatherer,
	}, promhttp.HandlerOpts{})

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

// =============================
// Config config structure
// =============================
type Config struct {
	Bind     string
	Type     string
	DSN      string
	Instance string
	Metrics  map[string]struct {
		Query       string
		Type        string
		Description string
		Labels      []string
		Value       string
		metricDesc  *prometheus.Desc
	}
}

// =============================
// QueryCollector exporter
// =============================
type QueryCollector struct {
	cfg *Config
}

// Describe prometheus describe
func (e *QueryCollector) Describe(ch chan<- *prometheus.Desc) {
}

// Collect prometheus collect
func (e *QueryCollector) Collect(ch chan<- prometheus.Metric) {
	// ========================
	// Connect to database
	// ========================
	db, err := sql.Open(e.cfg.Type, e.cfg.DSN)
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
		if err != nil {
			log.Errorf("Failed to get column meta: %s", err)
			continue
		}

		des := make([]interface{}, len(cols))
		res := make([][]byte, len(cols))
		for i := range cols {
			des[i] = &res[i]
		}

		// fetch database
		for rows.Next() {
			rows.Scan(des...)
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
