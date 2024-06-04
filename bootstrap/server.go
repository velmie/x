package bootstrap

import "context"

// Server is an interface that represents a server
type Server interface {
	// ListenAndServe starts the server
	ListenAndServe() error
	// Shutdown shuts down the server
	Shutdown(ctx context.Context) error
}

// ServerWrapper is a wrapper around a Server
type ServerWrapper struct {
	server Server
}

// NewServerWrapper creates a new ServerWrapper with the given server
func NewServerWrapper(server Server) *ServerWrapper {
	return &ServerWrapper{server: server}
}

// Start starts the server
func (s ServerWrapper) Start() error {
	return s.server.ListenAndServe()
}

// Stop stops the server
func (s ServerWrapper) Stop(ctx context.Context) error {
	// Call the underlying Shutdown method with the provided context
	return s.server.Shutdown(ctx)
}
