// Package domain defines the DomainHandler interface that each runtime domain
// must implement. This mirrors the RuntimeDomainHandler protocol in the Swift
// runtime and establishes the boundary across which Go replaces Swift.
package domain

import "github.com/go-chi/chi/v5"

// Handler owns a cohesive set of HTTP routes.
// Each domain is independently replaceable during the Phase 5 cutover.
type Handler interface {
	// Register mounts all routes owned by this domain onto the given router.
	Register(r chi.Router)
}
