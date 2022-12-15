module go.chromium.org/goma/server

go 1.16

require (
	cloud.google.com/go/compute v1.6.0
	cloud.google.com/go/errorreporting v0.2.0
	cloud.google.com/go/profiler v0.2.0
	cloud.google.com/go/pubsub v1.20.0
	cloud.google.com/go/storage v1.22.0
	contrib.go.opencensus.io/exporter/prometheus v0.4.2
	contrib.go.opencensus.io/exporter/stackdriver v0.13.14
	github.com/bazelbuild/remote-apis v0.0.0-20210520160108-3e385366f152
	github.com/bazelbuild/remote-apis-sdks v0.0.0-20201118210229-b732553f9d45
	github.com/fsnotify/fsnotify v1.5.1
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da
	github.com/gomodule/redigo v1.8.8
	github.com/google/go-cmp v0.5.9
	github.com/google/uuid v1.3.0
	github.com/googleapis/gax-go/v2 v2.3.0
	github.com/googleapis/google-cloud-go-testing v0.0.0-20190904031503-2d24dde44ba5
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0
	go.opencensus.io v0.23.0
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.28.0
	go.opentelemetry.io/otel v1.11.1
	go.opentelemetry.io/otel/bridge/opencensus v0.33.0
	go.opentelemetry.io/otel/exporters/jaeger v1.11.1
	go.opentelemetry.io/otel/sdk v1.11.1
	go.uber.org/zap v1.21.0
	golang.org/x/build v0.0.0-20191031202223-0706ea4fce0c
	golang.org/x/net v0.0.0-20220412020605-290c469a71a5
	golang.org/x/oauth2 v0.0.0-20220411215720-9780585627b5
	golang.org/x/sync v0.0.0-20220601150217-0de741cfad7f
	google.golang.org/api v0.75.0
	google.golang.org/genproto v0.0.0-20220414192740-2d67ff6cf2b4
	google.golang.org/grpc v1.45.0
	google.golang.org/protobuf v1.28.1
)
