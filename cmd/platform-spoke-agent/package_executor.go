package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/agent/adapters/uds"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

// The Zarf/UDS executor is off unless PACKAGE_EXECUTOR_ENABLED is "true". Each
// variable maps to the uds.Executor field of the same name:
//
//	PACKAGE_EXECUTOR_ENABLED                "true" turns the executor on
//	PACKAGE_PUBLISHER_CERTIFICATE_ISSUER    Publisher.CertificateIssuer   (anchored regexp)
//	PACKAGE_PUBLISHER_CERTIFICATE_IDENTITY  Publisher.CertificateIdentity (anchored regexp)
//	PACKAGE_VERIFICATION_KEY_PATH           VerificationKeyPath (public key file)
//	PACKAGE_ARCHITECTURE                    Architecture (default: this binary's)
//	UDS_NATIVE_BUNDLES                     NativeBundles (explicit NextMode opt-in)
//	ZARF_BIN, UDS_BIN, KUBECTL_BIN          ZarfBin, UDSBin, KubectlBin (default: PATH lookup)
//
// Exactly one of the publisher pair and the key path must be set: the executor
// runs nothing it cannot verify. A UDS bundle can only be verified with a key,
// so a Spoke configured with a publisher policy deploys Zarf packages and
// refuses bundles.
const (
	packageExecutorEnabledVariable = "PACKAGE_EXECUTOR_ENABLED"
	packageExecutorHint            = "set " + packageExecutorEnabledVariable + "=true and the package verification settings on this Spoke"
)

var packageExecutorVariables = []string{
	"PACKAGE_PUBLISHER_CERTIFICATE_ISSUER", "PACKAGE_PUBLISHER_CERTIFICATE_IDENTITY",
	"PACKAGE_VERIFICATION_KEY_PATH", "PACKAGE_ARCHITECTURE", "ZARF_BIN", "UDS_BIN", "UDS_NATIVE_BUNDLES", "UDS_REQUIRE_PACKAGE_STATUS",
}

// packageExecutorFromEnvironment builds the Zarf/UDS executor, or returns nil
// when the Spoke was not configured for one.
//
// Misconfiguration is an error rather than a quietly disabled executor: an
// operator who set a verification key and forgot the switch, or flipped the
// switch with nothing to verify against, should learn that at start-up and
// not from the first Plan that needs it.
func packageExecutorFromEnvironment(getenv func(string) string) (agent.Executor, error) {
	value := func(name string) string { return strings.TrimSpace(getenv(name)) }
	switch enabled := value(packageExecutorEnabledVariable); enabled {
	case "", "false":
		for _, name := range packageExecutorVariables {
			if value(name) != "" {
				return nil, fmt.Errorf("%s is set but %s is not \"true\"", name, packageExecutorEnabledVariable)
			}
		}
		return nil, nil
	case "true":
	default:
		return nil, fmt.Errorf("%s must be \"true\" or \"false\", not %q", packageExecutorEnabledVariable, enabled)
	}

	executor := uds.Executor{
		ZarfBin: value("ZARF_BIN"), UDSBin: value("UDS_BIN"), KubectlBin: value("KUBECTL_BIN"),
		Architecture: value("PACKAGE_ARCHITECTURE"),
		Publisher: domain.PublisherPolicy{
			CertificateIssuer:   value("PACKAGE_PUBLISHER_CERTIFICATE_ISSUER"),
			CertificateIdentity: value("PACKAGE_PUBLISHER_CERTIFICATE_IDENTITY"),
		},
		VerificationKeyPath: value("PACKAGE_VERIFICATION_KEY_PATH"),
	}
	switch native := value("UDS_NATIVE_BUNDLES"); native {
	case "", "false":
	case "true":
		executor.NativeBundles = true
	default:
		return nil, errors.New("UDS_NATIVE_BUNDLES must be true or false")
	}
	switch required := value("UDS_REQUIRE_PACKAGE_STATUS"); required {
	case "", "false":
	case "true":
		executor.RequirePackageStatus = true
	default:
		return nil, errors.New("UDS_REQUIRE_PACKAGE_STATUS must be true or false")
	}
	keyless := executor.Publisher != (domain.PublisherPolicy{})
	switch {
	case keyless && executor.VerificationKeyPath != "":
		return nil, errors.New("set either the PACKAGE_PUBLISHER_CERTIFICATE_* pair or PACKAGE_VERIFICATION_KEY_PATH, not both")
	case keyless:
		if err := executor.Publisher.Validate(); err != nil {
			return nil, fmt.Errorf("package publisher policy: %w", err)
		}
	case executor.VerificationKeyPath != "":
		if !filepath.IsAbs(executor.VerificationKeyPath) {
			return nil, errors.New("PACKAGE_VERIFICATION_KEY_PATH must be an absolute path")
		}
		if info, err := os.Stat(executor.VerificationKeyPath); err != nil || info.IsDir() {
			return nil, fmt.Errorf("PACKAGE_VERIFICATION_KEY_PATH %s is not a readable file", executor.VerificationKeyPath)
		}
	default:
		return nil, fmt.Errorf("%s=true requires the PACKAGE_PUBLISHER_CERTIFICATE_* pair or PACKAGE_VERIFICATION_KEY_PATH", packageExecutorEnabledVariable)
	}
	if arch := executor.Architecture; arch != "" && arch != "amd64" && arch != "arm64" {
		return nil, fmt.Errorf("PACKAGE_ARCHITECTURE must be amd64 or arm64, not %q", arch)
	}
	return executor, nil
}

// stepExecutor is the executor the worker runs: Helm for every step that
// names no artifact kind, and the package executor -- when there is one -- for
// Zarf packages and UDS bundles.
func stepExecutor(helm, packages agent.Executor) agent.KindExecutor {
	return agent.KindExecutor{Helm: helm, Package: packages, PackageHint: packageExecutorHint}
}
