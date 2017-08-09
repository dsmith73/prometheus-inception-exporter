package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
	"github.com/prometheus/prometheus/web/api/v1"
)

const (
	target_down      = 0
	target_up        = 1
	target_disappear = 2
)

var (
	metrics_namespace = "prometheus_inception"

	target_count  = newGauge("target_count", "Number of targets on Prometheus instance", nil)
	target_states = map[string]prometheus.Gauge{} // newGauge("target_state", "Current targets state", nil)
)

func newGauge(metricName string, docString string, constLabels prometheus.Labels) prometheus.Gauge {
	return prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace:   metrics_namespace,
			Name:        metricName,
			Help:        docString,
			ConstLabels: constLabels,
		},
	)
}

// Exporter collects Prometheus targets states from the given hostname and exports them using
// the prometheus metrics package.
type Exporter struct {
	mutex             sync.RWMutex
	URI               string
	BasicAuthUsername string
	BasicAuthPassword string
	client            *http.Client
}

// NewExporter returns an initialized Exporter.
func NewExporter(uri string, username string, password string, timeout time.Duration) *Exporter {
	// Set up our Prometheus client connection.
	return &Exporter{
		URI:               uri,
		BasicAuthUsername: username,
		BasicAuthPassword: password,
		client: &http.Client{
			Transport: &http.Transport{
				Dial: func(netw, addr string) (net.Conn, error) {
					c, err := net.DialTimeout(netw, addr, timeout)
					if err != nil {
						return nil, err
					}
					if err := c.SetDeadline(time.Now().Add(timeout)); err != nil {
						return nil, err
					}
					return c, nil
				},
			},
		},
	}
}

// Describe describes all the metrics ever exported by the Prometheus exporter. It
// implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- target_count.Desc()
	for _, metric := range target_states {
		ch <- metric.Desc()
	}
}

// Collect fetches the stats from configured Prometheus location and delivers them
// as Prometheus metrics. It implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock() // To protect metrics from concurrent collects.
	defer e.mutex.Unlock()

	if err := e.scrape(); err != nil {
		log.Error(err)
		target_count.Set(0)
		ch <- target_count
		return
	}

	for _, metric := range target_states {
		ch <- metric
	}
	ch <- target_count
}

type prometheusResponse struct {
	Status    string             `json:"status"`
	Data      v1.TargetDiscovery `json:"data,omitempty"`
	ErrorType string             `json:"errorType,omitempty"`
	Error     string             `json:"error,omitempty"`
}

func (e *Exporter) scrape() error {
	req, err := http.NewRequest("GET", e.URI+"/api/v1/targets", nil)
	if err != nil {
		return errors.New(fmt.Sprintf("Can't scrape Prometheus api: %v", err))
	}

	req.SetBasicAuth(e.BasicAuthUsername, e.BasicAuthPassword)

	resp, err := e.client.Do(req)
	if err != nil {
		return errors.New(fmt.Sprintf("Can't scrape Prometheus api: %v", err))
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.New(fmt.Sprintf("Can't scrape Prometheus api: %v", err))
	}
	fmt.Println(string(body))

	var pr prometheusResponse
	err = json.Unmarshal(body, &pr)
	if err != nil {
		return errors.New(fmt.Sprintf("Can't scrape Prometheus api: %v", err))
	}

	// Must be reset for disabled targets
	for _, metric := range target_states {
		metric.Set(target_disappear)
	}

	count := 0
	for _, target := range pr.Data.ActiveTargets {
		if _, ok := target_states[target.ScrapeURL]; ok == false {
			target_states[target.ScrapeURL] = newGauge("target_state", "Prometheus targets state", prometheus.Labels{"job_name": string(target.DiscoveredLabels["job"]), "scrape_url": target.ScrapeURL})
		}

		if target.Health == "up" {
			target_states[target.ScrapeURL].Set(target_up)
			count++
		} else if target.Health == "down" || target.Health == "unknown" {
			target_states[target.ScrapeURL].Set(target_down)
			count++
		}
	}

	target_count.Set(float64(count))

	return nil
}

func init() {
	prometheus.MustRegister(version.NewCollector("prometheus_inception_exporter"))
	target_count.Set(0)
}

func main() {
	var (
		showVersion        = flag.Bool("version", false, "Print version information.")
		listenAddress      = flag.String("web.listen-address", ":9142", "Address to listen on for web interface and telemetry.")
		metricsPath        = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		namespace          = flag.String("namespace", metrics_namespace, "Namespace for metrics")
		prometheusAddr     = flag.String("prometheus.address", "http://localhost:9090", "HTTP API address of Prometheus instance.")
		prometheusUsername = flag.String("prometheus.basic_auth.username", "", "Username of Prometheus instance.")
		prometheusPassword = flag.String("prometheus.basic_auth.password", "", "Password of Prometheus instance.")
		timeout            = flag.Duration("timeout", 5, "Timeout for trying to get states from Prometheus.")
	)
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stdout, version.Print("prometheus_inception_exporter"))
		os.Exit(0)
	}

	log.Infoln("Starting prometheus_inception_exporter", version.Info())
	log.Infoln("Build context", version.BuildContext())

	metrics_namespace = *namespace

	exporter := NewExporter(*prometheusAddr, *prometheusUsername, *prometheusPassword, *timeout*time.Second)
	prometheus.MustRegister(exporter)
	// prometheus.Unregister(prometheus.NewGoCollector())
	// prometheus.Unregister(prometheus.NewProcessCollector(os.Getpid(), ""))

	http.Handle(*metricsPath, prometheus.UninstrumentedHandler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
<head><title>Prometheus Inception Exporter</title></head>
<body>
<h1>Prometheus Inception Exporter</h1>
<p><a href='` + *metricsPath + `'>Metrics</a></p>
</body>
</html>`))
	})

	log.Infoln("Listening on", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
