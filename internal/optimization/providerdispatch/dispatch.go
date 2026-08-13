// Package providerdispatch composes provider-specific hexagonal adapters
// without giving the optimization domain transport, credential, or discovery
// capabilities.
package providerdispatch

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

// ReadBinding grants one source to one outbound read port. A source cannot be
// registered twice: failover and fallback must remain explicit acquisition
// calls with independently attributable receipts.
type ReadBinding struct {
	Source string
	Port   optimization.AcquisitionReadPort
}

type Reader struct {
	ports map[string]optimization.AcquisitionReadPort
}

func NewReader(bindings []ReadBinding) (Reader, error) {
	ports := make(map[string]optimization.AcquisitionReadPort, len(bindings))
	for _, binding := range bindings {
		if binding.Source == "" || binding.Port == nil {
			return Reader{}, errors.New("provider reader binding requires source and port")
		}
		if _, exists := ports[binding.Source]; exists {
			return Reader{}, fmt.Errorf("provider reader source %q is registered more than once", binding.Source)
		}
		ports[binding.Source] = binding.Port
	}
	if len(ports) == 0 {
		return Reader{}, errors.New("provider reader requires at least one binding")
	}
	return Reader{ports: ports}, nil
}

func (reader Reader) Read(ctx context.Context, request optimization.AcquisitionReadRequest) (optimization.AcquisitionReadResponse, error) {
	port := reader.ports[request.Source]
	if port == nil {
		return optimization.AcquisitionReadResponse{}, optimization.AcquisitionReadError{Category: optimization.AcquisitionFailureAuthorization}
	}
	return port.Read(ctx, request)
}

// ExtractBinding binds an anti-corruption adapter to one complete request
// identity. Endpoint aliases and source-wide switches are intentionally not
// supported because they can silently reinterpret facts after a registry edit.
type ExtractBinding struct {
	CallID    string
	Source    string
	Endpoint  string
	Extractor optimization.AcquisitionFactExtractor
}

type Extractor struct {
	bindings map[string]ExtractBinding
}

func NewExtractor(bindings []ExtractBinding) (Extractor, error) {
	byCall := make(map[string]ExtractBinding, len(bindings))
	for _, binding := range bindings {
		if binding.CallID == "" || binding.Source == "" || binding.Endpoint == "" || binding.Extractor == nil {
			return Extractor{}, errors.New("provider extractor binding requires call, source, endpoint, and extractor")
		}
		if _, exists := byCall[binding.CallID]; exists {
			return Extractor{}, fmt.Errorf("provider extractor call %q is registered more than once", binding.CallID)
		}
		byCall[binding.CallID] = binding
	}
	if len(byCall) == 0 {
		return Extractor{}, errors.New("provider extractor requires at least one binding")
	}
	return Extractor{bindings: byCall}, nil
}

func (extractor Extractor) Extract(request optimization.AcquisitionReadRequest, response optimization.AcquisitionReadResponse) ([]optimization.ExtractedAcquisitionFact, error) {
	binding, exists := extractor.bindings[request.ID]
	if !exists || binding.Source != request.Source || binding.Endpoint != request.Endpoint {
		return nil, errors.New("provider extractor request is not exactly registered")
	}
	return binding.Extractor.Extract(request, response)
}

var _ optimization.AcquisitionReadPort = Reader{}
var _ optimization.AcquisitionFactExtractor = Extractor{}
