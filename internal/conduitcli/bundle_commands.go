package conduitcli

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	artifacts "github.com/jonathanagustin/conduit/internal/artifacts/domain"
	"github.com/spf13/cobra"
)

// maximumBundleBytes matches the body limit on the Hub's bundle preview and
// import routes. A larger file is one those routes would refuse.
const maximumBundleBytes = 1 << 20

// bundleRejectedStatus is the process status for a Bundle the shared verifier
// refused. A missing file or an unreadable trust map stays status 1.
const bundleRejectedStatus = 2

func bundleCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{
		Use:   "bundle",
		Short: "Inspect a Bundle without contacting the Hub",
	}
	var keysFile string
	verify := &cobra.Command{
		Use:   "verify PATH",
		Short: "Verify a Bundle offline with the Hub verifier",
		Long: `Verify PATH with the envelope verifier the Hub uses before it will trust a Bundle.

The trusted-keys file is the JSON map of signing-key id to base64 Ed25519 public key that a Hub reads. This command does not contact a Hub and does not admit the Bundle.

Exit status 0 means the verifier accepted the Bundle. Exit status 2 means the verifier rejected it. Exit status 1 means the Bundle or the trust file could not be read.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return verifyBundle(state, args[0], keysFile)
		},
	}
	verify.Flags().StringVar(&keysFile, "trusted-keys", state.runtime.Getenv("BUNDLE_TRUSTED_KEYS_FILE"), "JSON map of trusted Bundle signing keys (BUNDLE_TRUSTED_KEYS_FILE)")
	command.AddCommand(verify)
	return command
}

func verifyBundle(state *commandState, path, keysFile string) error {
	contents, err := readBundle(state, path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var envelope artifacts.Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("decode Bundle: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("Bundle file must contain exactly one JSON document")
	}
	trusted, err := trustedBundleKeys(keysFile)
	if err != nil {
		return err
	}
	if err := artifacts.VerifyEnvelope(envelope, trusted); err != nil {
		return StatusError{Status: bundleRejectedStatus, Err: err}
	}
	return writeOutput(state.runtime.Stdout, state.output, bundleVerification{
		Verified: true, BundleID: envelope.Manifest.ID,
		ManifestDigest: envelope.ManifestDigest, SigningKeyID: envelope.SigningKeyID,
	})
}

func readBundle(state *commandState, path string) ([]byte, error) {
	var input io.Reader
	if path == "-" {
		input = state.runtime.Stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open Bundle: %w", err)
		}
		defer file.Close()
		input = file
	}
	contents, err := io.ReadAll(io.LimitReader(input, maximumBundleBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Bundle: %w", err)
	}
	if len(contents) > maximumBundleBytes {
		return nil, fmt.Errorf("Bundle exceeds %d bytes", maximumBundleBytes)
	}
	return contents, nil
}

func trustedBundleKeys(path string) (map[string]ed25519.PublicKey, error) {
	if path == "" {
		return nil, errors.New("trusted Bundle keys are required; set --trusted-keys or BUNDLE_TRUSTED_KEYS_FILE")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read trusted Bundle keys: %w", err)
	}
	keys, err := artifacts.ParseTrustedKeys(contents)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, errors.New("trusted Bundle keys are required")
	}
	for id, key := range keys {
		if id != artifacts.SigningKeyID(key) {
			return nil, fmt.Errorf("invalid trusted Bundle signing key %q", id)
		}
	}
	return keys, nil
}

// StatusError is a command failure whose process status is not the default 1.
type StatusError struct {
	Status int
	Err    error
}

func (err StatusError) Error() string {
	if err.Err == nil {
		return fmt.Sprintf("exit status %d", err.Status)
	}
	return err.Err.Error()
}

func (err StatusError) Unwrap() error { return err.Err }

func (err StatusError) ExitCode() int { return err.Status }
