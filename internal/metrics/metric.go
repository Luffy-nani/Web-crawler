package metric

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	PagesFetched     metric.Int64Counter
	FetchDuration    metric.Float64Histogram
	ChangesDetected  metric.Int64Counter
	PagesIndexed     metric.Int64Counter
}

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

	return m, promhttp.Handler(), nil
}