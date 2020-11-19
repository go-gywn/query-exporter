package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

const (
	namespace = "query"
	exporter  = "exporter"
)

// QueryExporter query exporter collector
type QueryExporter struct {
	collector Collector
}

// Describe prometheus describe
func (e *QueryExporter) Describe(ch chan<- *prometheus.Desc) {
}

// Collect prometheus collect
func (e *QueryExporter) Collect(ch chan<- prometheus.Metric) {
	for _, name := range e.collector.Targets {
		for _, instance := range group[name] {
			e.scrape(instance, ch)
		}
	}
}

// scrape connnect to database and gather query result
func (e *QueryExporter) scrape(instance Instance, ch chan<- prometheus.Metric) {

	// Gconnect to database
	conInfo := fmt.Sprintf("%s:%s@%s", instance.User, instance.Pass, instance.DSN)
	db, err := sql.Open(instance.Type, conInfo)
	if err != nil {
		log.Errorf("[%s] Connect to %s database failed: %s", instance.Instance, instance.Type, err)
		return
	}
	defer db.Close()

	for _, collect := range e.collector.Collects {
		log.Debugf("Execute - %s", collect.Query)
		rows, err := db.Query(collect.Query)
		if err != nil {
			log.Errorf("[%s] Failed to execute query: %s", instance.Instance, err)
			continue
		}

		cols, err := rows.Columns()
		des := make([]interface{}, len(cols))
		res := make([][]byte, len(cols))

		for i := range cols {
			des[i] = &res[i]
		}

		for rows.Next() {
			err = rows.Scan(des...)
			data := make(map[string]string)
			for i, bytes := range res {
				data[cols[i]] = string(bytes)
			}
			data["instance"] = instance.Instance

			for _, metric := range collect.Metrics {
				labelVals := []string{}
				for _, label := range metric.Labels {
					labelVals = append(labelVals, data[label])
				}

				val, _ := strconv.ParseFloat(data[metric.Value], 64)
				switch strings.ToLower(metric.Type) {
				case "counter":
					ch <- prometheus.MustNewConstMetric(metric.metricDesc, prometheus.CounterValue, val, labelVals...)
				case "guage":
					ch <- prometheus.MustNewConstMetric(metric.metricDesc, prometheus.GaugeValue, val, labelVals...)
				default:
					log.Errorf("[%s] Metric type support only counter|guage, skip", instance.Instance)
					continue
				}
			}

		}
	}
}
