# Bootstrap
Package helps to automate application services startup and graceful shutdown. Pretty often `main` function is polluted with initialization of graceful shutdown channel, wait groups waiting, goroutine scheduling for each service. In conjunction, it adds a lot of nasty code and, moreover, pretty the same logic a repeated on each application you write. This Package helps to hide most of the logic under the hood.

## Usage
Firstly you have to create `Orchestrator` with options applicable for you.
```go
orc := bootstrap.NewOrchestrator(
    bootstrap.WithStopSignals(syscall.SIGINT, syscall.SIGTERM),
    bootstrap.WithLogger(bootstrap.NewDefaultLogger()),
    bootstrap.WithShutdownTimeout(5 * time.Second),
)
```
There are 3 options available for `Orchestrator` configuration:
* `bootstrap.WithStopSignals` - allows to specify signals which are considered as stop signals by `Orchestrator`. By default, `syscall.SIGINT`, `syscall.SIGTERM` signals are considered as stop signals;
* `bootstrap.WithLogger` - allows to set logger. Provided logger must implement `bootstrap.Logger` interface. If not specified, `DefaultLogger` is used for logging. Package also contains `NoopLogger` which shadows any logging message, so no logging output produced;
* `bootstrap.WithShutdownTimeout` - sets service shutdown timeout (timeout for each service to be stopped). There is no default timeout.

After that services must be registered. First option is to implement `bootstrap.Service` interface like it is done for `http.Server` below:
```go
// bootstrap.Service interface looks as follows:
// type Service interface {
//     Start() error
//     Stop(ctx context.Context) error
// }

type HTTPService struct {
    srv *http.Server
}

func NewHTTPService() *HTTPService {
    return &HTTPService{srv: &http.Server{}}
}

func (s *HTTPService) Start() error {
    return s.srv.ListenAndServe()
}

func (s *HTTPService) Stop(ctx context.Context) error {
    return s.srv.Shutdown(ctx)
}
```
After that it can be registered as follows:
```go
orc := bootstrap.NewOrchestrator()
orc.Register(NewHTTPService())
```
Such approach - implementation of `bootstrap.Service` might cause overhead and pollution of code, so there is a possibility to define start and stop functions and register them via `bootstrap.ServiceFunc`:
```go
srv := &http.Server{}

startFn := func() error {
    return srv.ListenAndServe()
}

stopFn := func(ctx context.Context) error {
    return srv.Shutdown(ctx)
}

orc := bootstrap.NewOrchestrator()
orc.Register(bootstrap.ServiceFunc(startFn, stopFn))
```
After that, services can be started as simply as follows:
```go
if err := orc.Serve(); err != nil {
    // handle error
}
```
`Serve` function calls start function of each service in separate goroutine. Please note, if at least one of the registered services fail to start, stop functions of each service will be called and error returned. `Serve` function is blocking and can be interrupted either via sending stop signal to application process or calling `Stop` function of `Orchestrator`.  
After sending stop signal to application process, shutdown process starts and stop functions are called for each service with timeout context if configured correspondingly.