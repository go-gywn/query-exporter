package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

const (
	namespace = "query"
	exporter  = "exporter"
)

// QueryCollector query exporter collector
type QueryCollector struct {
	instances  Instances
	collects   []Collect
	StatusDesc *prometheus.Desc
}

// Describe prometheus describe
func (e *QueryCollector) Describe(ch chan<- *prometheus.Desc) {
}

// Collect prometheus collect
func (e *QueryCollector) Collect(ch chan<- prometheus.Metric) {
	for _, instance := range e.instances {
		e.scrape(*instance, ch)
	}
}

// scrape connnect to database and gather query result
func (e *QueryCollector) scrape(instance Instance, ch chan<- prometheus.Metric) {

	// Collector status
	var collectStatus float64
	defer func() {
		log.Debugf("[%s] collector status: %d", instance.Name, collectStatus)
		ch <- prometheus.MustNewConstMetric(e.StatusDesc, prometheus.GaugeValue, collectStatus, instance.Name)
	}()

	// Connect to database
	connectionString := fmt.Sprintf("%s:%s@%s", instance.User, instance.Pass, instance.DSN)
	db, err := sql.Open(instance.Type, connectionString)
	if err != nil {
		log.Errorf("[%s] Connect to %s database failed: %s", instance.Name, instance.Type, err)
		return
	}
	defer db.Close()

	// Connection check
	if err := db.Ping(); err != nil {
		log.Errorf("[%s] Connect to %s database failed: %s", instance.Name, instance.Type, err)
		return
	}

	// Execute collect queries, and make metrics for the result
	for _, collect := range e.collects {
		log.Debugf("[%s] execute query: %s", instance.Name, collect.Query)

		// // Query timeout
		// ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		// defer cancel()
		// rows, err := db.QueryContext(ctx, collect.Query)

		rows, err := db.Query(collect.Query)
		if err != nil {
			log.Errorf("[%s] Failed to execute query: %s", instance.Name, err)
			continue
		}

		cols, err := rows.Columns()
		if err != nil {
			log.Errorf("[%s] Failed to get column info: %s", instance.Name, err)
			continue
		}
		log.Debugf("[%s] cols - %s", instance.Name, cols)

		des := make([]interface{}, len(cols))
		res := make([][]byte, len(cols))

		for i := range cols {
			des[i] = &res[i]
		}

		for rows.Next() {
			if err = rows.Scan(des...); err != nil {
				log.Errorf("[%s] row scan error, break rows.Nexe(): %s", instance.Name, err)
				break
			}

			data := make(map[string]string)
			for i, bytes := range res {
				data[cols[i]] = string(bytes)
			}
			data["instance"] = instance.Name

			for _, metric := range collect.Metrics {
				log.Debugf("[%s] metric labels: %s", instance.Name, metric.metricDesc)
				labelVals := []string{}
				for _, label := range metric.Labels {
					labelVals = append(labelVals, data[label])
				}
				log.Debugf("[%s] metric values: %s", instance.Name, labelVals)

				val, _ := strconv.ParseFloat(data[metric.Value], 64)
				switch strings.ToLower(metric.Type) {
				case "counter":
					ch <- prometheus.MustNewConstMetric(metric.metricDesc, prometheus.CounterValue, val, labelVals...)
				case "guage":
					ch <- prometheus.MustNewConstMetric(metric.metricDesc, prometheus.GaugeValue, val, labelVals...)
				default:
					log.Errorf("[%s] Metric type support only counter|guage, skip", instance.Name)
					continue
				}
			}
		}
	}
	collectStatus = 1
}
