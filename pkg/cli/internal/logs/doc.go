// Package logs is a copy of kubernetes/staging/src/k8s.io/kubectl/pkg/cmd/logs,
// which contains LogOptions.RunLogsContext function that is needed for proper signal handling.
// This is not yet available in v33.
//
// TODO: Remove and replace once deps are updated to future v34.
package logs
