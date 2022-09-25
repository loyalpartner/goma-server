// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"contrib.go.opencensus.io/exporter/stackdriver/propagation"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"

	"go.chromium.org/goma/server/log"

	"contrib.go.opencensus.io/exporter/zipkin"
	openzipkin "github.com/openzipkin/zipkin-go"
	zipkinHTTP "github.com/openzipkin/zipkin-go/reporter/http"
)

var (
	exporter *stackdriver.Exporter

	// Increased from Default 10 seconds for quota limit.
	// The recommended reporting period by Stackdriver Monitoring is >= 1 minute:
	// https://cloud.google.com/monitoring/custom-metrics/creating-metrics#writing-ts
	// https://pkg.go.dev/go.opencensus.io/stats/view?tab=doc#SetReportingPeriod
	reportingInterval = 90 * time.Second
)

// Init initializes opencensus instrumentations, and error reporter.
// If projectID is not empty, it registers stackdriver exporter for the project.
// It also calls SetupHTTPClient.
func Init(ctx context.Context, projectID, name string) error {
	logger := log.FromContext(ctx)
	localEndpoint, err := openzipkin.NewEndpoint("goma-server", "192.168.1.5:5454")
	if err != nil {
		logger.Fatalf("Failed to create the local zipkinEndpoint: %v", err)
	}
	reporter := zipkinHTTP.NewReporter("http://10.72.230.126:9411/api/v2/spans")
	exporter := zipkin.NewExporter(reporter, localEndpoint)
	trace.RegisterExporter(exporter)

	// 2. Configure 100% sample rate, otherwise, few traces will be sampled.
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})

	if projectID != "" {
		// logger.Infof("send stackdriver trace log to project %s", projectID)

		// var err error
		// exporter, err = stackdriver.NewExporter(stackdriver.Options{
		// 	ProjectID: projectID,
		// 	OnError: func(err error) {
		// 		switch status.Code(err) {
		// 		case codes.Unavailable:
		// 			logger.Warnf("Failed to export to Stackdriver: %v", err)
		// 		default:
		// 			logger.Errorf("Failed to export to Stackdriver: %v", err)
		// 		}
		// 	},
		// 	MonitoredResource: monitoredresource.Autodetect(),

		// 	// Disallow grpc in google-api-go-client to send stats/trace of monitoring grpc's api call.
		// 	MonitoringClientOptions: []option.ClientOption{option.WithGRPCDialOption(grpc.WithStatsHandler(nil))},
		// 	TraceClientOptions:      []option.ClientOption{option.WithGRPCDialOption(grpc.WithStatsHandler(nil))},
		// })
		// if err != nil {
		// 	return fmt.Errorf("failed to create exporter: %v", err)
		// }
		// view.RegisterExporter(exporter)
		// trace.RegisterExporter(exporter)
		// view.SetReportingPeriod(reportingInterval)

		// errorreporter.DefaultErrorReporter = errorreporter.New(ctx, projectID, serverName(ctx, name))
	}

	err = view.Register(ocgrpc.DefaultServerViews...)
	if err != nil {
		return fmt.Errorf("failed to subscribe ocgrpc view: %v", err)
	}
	err = view.Register(ocgrpc.DefaultClientViews...)
	if err != nil {
		return fmt.Errorf("failed to subscribe ocgrpc client view: %v", err)
	}
	err = view.Register(ochttp.DefaultServerViews...)
	if err != nil {
		return fmt.Errorf("failed to subscribe ochttp view: %v", err)
	}
	err = view.Register(ochttp.DefaultClientViews...)
	if err != nil {
		return fmt.Errorf("failed to subscribe ochttp view: %v", err)
	}
	SetupHTTPClient()

	err = view.Register(procStatViews...)
	if err != nil {
		return fmt.Errorf("failed to subscribe proc stat view: %v", err)
	}
	go reportProcStats(context.Background())
	return nil
}

// SetupHTTPClient sets up http default client to monitor by opencensus.
func SetupHTTPClient() {
	// we can't set the transport as http.DefaultTransport, because
	// ochttp.Transport will use http.DefaultTransport so it causes
	// recursive calls.
	http.DefaultClient = &http.Client{
		Transport: &ochttp.Transport{
			Propagation: &propagation.HTTPFormat{},
		},
	}
}

// Flush flushes opencensus data.
func Flush() {
	if exporter == nil {
		return
	}
	exporter.Flush()
}
