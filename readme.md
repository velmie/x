# Velmie common packages

The repository contains common packages used in Velmie projects.

## Packages

- [AppError](./svc/apperror) - describes transport-neutral application errors
- [Authentication](./authentication) - contains authentication related features
- [Bootstrap](./bootstrap) - contains application services startup and graceful shutdown automation logic
- [ConnectX](./svc/connectx) - maps application errors to Connect errors
- [SQLTx](./sqltx) - contains a wrapper that allows working with sql transactions through Go context
- [EnvX](./envx) - provides fluent API for retrieving and validating environment variables
- [ipX](./ipx) - provides functionality to obtain the real IP address
- [SlogX](./svc/slogx) - provides attribute redaction for log/slog handlers
