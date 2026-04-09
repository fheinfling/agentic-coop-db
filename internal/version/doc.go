// Package version contains build-time identifiers injected with -ldflags.
//
// All vars in this package are intentionally simple strings so that
//
//	go build -ldflags "-X github.com/fheinfling/agentic-coop-db/internal/version.Version=$VERSION ..."
//
// works without reflection or init() side effects.
package version
