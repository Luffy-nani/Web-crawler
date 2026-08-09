package metrics

import (
	"context"
	"net/http"
	"sync"

	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"webcrawler/internal/frontier"
)

type Metrics struct {
	PagesFetched    metric.Int64Counter
	FetchDuration   metric.Float64Histogram
	ChangesDetected metric.Int64Counter
	PagesIndexed    metric.Int64Counter

	frontierMu sync.Mutex
	frontier   *frontier.Frontier // whichever cycle's Frontier is currently active
}

// SetFrontier tells the queue-depth gauge which Frontier to read from.
// Call this at the start of every runCrawlCycle, since a new Frontier
// is created each cycle (fresh dedup set - see Phase 5).
func (m *Metrics) SetFrontier(f *frontier.Frontier) {
	m.frontierMu.Lock()
	defer m.frontierMu.Unlock()
	m.frontier = f
}

// New sets up the OTel -> Prometheus pipeline and returns both the
// recordable instruments and the HTTP handler to serve /metrics from.
func New() (*Metrics, http.Handler, error) {
	exporter, err := prometheus.New()
	if err != nil {
		return nil, nil, err
	}

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	meter := provider.Meter("webcrawler")

	pagesFetched, err := meter.Int64Counter("crawler_pages_fetched_total",
		metric.WithDescription("Total pages fetched, labeled by status"))
	if err != nil {
		return nil, nil, err
	}

	fetchDuration, err := meter.Float64Histogram("crawler_fetch_duration_seconds",
		metric.WithDescription("Fetch latency in seconds"))
	if err != nil {
		return nil, nil, err
	}

	changesDetected, err := meter.Int64Counter("crawler_changes_detected_total",
		metric.WithDescription("Total meaningful changes detected"))
	if err != nil {
		return nil, nil, err
	}

	pagesIndexed, err := meter.Int64Counter("crawler_pages_indexed_total",
		metric.WithDescription("Total pages chunked and embedded"))
	if err != nil {
		return nil, nil, err
	}

	m := &Metrics{
		PagesFetched:    pagesFetched,
		FetchDuration:   fetchDuration,
		ChangesDetected: changesDetected,
		PagesIndexed:    pagesIndexed,
	}

	// Queue depth gauge - notice this has NO .Add() or .Record() call
	// anywhere. Instead, we register the gauge itself here, then
	// separately register a CALLBACK below that fires on every scrape.
	queueDepth, err := meter.Int64ObservableGauge("crawler_frontier_queue_depth",
		metric.WithDescription("Current number of URLs queued across all hosts"))
	if err != nil {
		return nil, nil, err
	}

	// RegisterCallback wires the callback function to the gauge. Every
	// time Prometheus scrapes /metrics, OTel calls this function fresh -
	// so it always reports whatever m.frontier's queue depth is AT THAT
	// MOMENT, not a stale value from whenever New() ran.
	_, err = meter.RegisterCallback(func(_ context.Context, obs metric.Observer) error {
		m.frontierMu.Lock()
		f := m.frontier
		m.frontierMu.Unlock()

		if f != nil {
			obs.ObserveInt64(queueDepth, int64(f.QueueDepth()))
		}
		return nil
	}, queueDepth)
	if err != nil {
		return nil, nil, err
	}

	return m, promhttp.Handler(), nil
}