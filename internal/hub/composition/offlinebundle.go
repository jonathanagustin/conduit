package composition

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	offlineapp "github.com/jonathanagustin/conduit/internal/delivery/offlinebundle/application"
	offlinehttp "github.com/jonathanagustin/conduit/internal/delivery/offlinebundle/httpapi"
)

type bundleVerifier struct {
	repository controlplane.Repository
	key        ed25519.PrivateKey
	trusted    map[string]ed25519.PublicKey
}

func (verifier bundleVerifier) Verify(ctx context.Context, bundleID string) (string, error) {
	bundles, err := controlplane.NewBundleService(verifier.repository, verifier.key, verifier.trusted)
	if err != nil {
		return "", err
	}
	envelope, err := verifier.repository.GetBundle(ctx, bundleID)
	if err != nil {
		return "", err
	}
	preview, err := bundles.Verification(ctx, envelope)
	if err != nil {
		return "", err
	}
	if preview.Signature.Status != controlplane.SignatureVerified {
		return "", errUnverified
	}
	return preview.ManifestDigest, nil
}

type errUnverifiedT struct{}

func (errUnverifiedT) Error() string { return "bundle signature did not verify" }

var errUnverified = errUnverifiedT{}

type sitePresence struct {
	repository controlplane.Repository
	now        func() time.Time
}

func (presence sitePresence) Online(ctx context.Context, site string) (bool, error) {
	report, err := presence.repository.GetEnvironmentReport(ctx, site)
	if errors.Is(err, controlplane.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if report.Environment != site {
		return false, nil
	}
	now := time.Now()
	if presence.now != nil {
		now = presence.now()
	}
	return controlplane.ReportedSpokeStatus(&report, now).State == controlplane.SpokeLive, nil
}

func mountOfflineBundles(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	service := &offlineapp.Service{
		Store:      repository,
		Governance: newGovernanceService(repository, options),
		Verifier:   bundleVerifier{repository: repository, key: options.BundlePrivateKey, trusted: options.TrustedBundleKeys},
		Presence:   sitePresence{repository: repository},
	}
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	offlinehttp.Register(mux, offlinehttp.Config{
		Authorize: func(request *http.Request) error {
			_, err := operatorClaims(request, options)
			return err
		},
		Write: func(request *http.Request) (int, any, error) {
			principal, ok := packagingPrincipal(request, governance, options)
			if !ok {
				return http.StatusUnauthorized, map[string]string{"error": "unauthorized"}, nil
			}
			if !governance.AuthorizeResourceOperation(request.Context(), principal, options, "platform/root", controlplane.OperationOperate) {
				return http.StatusForbidden, map[string]string{"error": "offline bundle operations require platform operate permission"}, nil
			}
			var body struct {
				Action    string `json:"action"`
				BundleID  string `json:"bundleId"`
				Site      string `json:"site"`
				ID        string `json:"id"`
				Outcome   string `json:"outcome"`
				Confirm   bool   `json:"confirm"`
				ChangeID  string `json:"changeRequestId"`
				Rationale string `json:"rationale"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(nil, request.Body, 1<<16))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&body); err != nil {
				return http.StatusBadRequest, map[string]string{"error": "request body is not valid JSON"}, nil
			}
			if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
				return http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"}, nil
			}
			switch body.Action {
			case "handoff":
				handoff, reason, err := service.HandOff(request.Context(), principal.Username, body.BundleID, body.Site, body.Confirm)
				if err != nil {
					return 0, nil, err
				}
				status := http.StatusCreated
				if reason == "confirmation is required" {
					status = http.StatusConflict
				}
				return status, map[string]any{"handoff": handoff, "reason": reason}, nil
			case "report":
				handoff, reason, err := service.ProposeReport(request.Context(), principal, body.ID, body.Outcome)
				if err != nil {
					return 0, nil, err
				}
				return http.StatusAccepted, map[string]any{"handoff": handoff, "reason": reason}, nil
			case "approve":
				handoff, reason, err := service.ApproveInstall(request.Context(), principal, body.ChangeID, body.Rationale)
				if err != nil {
					return 0, nil, err
				}
				return http.StatusOK, map[string]any{"handoff": handoff, "reason": reason}, nil
			default:
				return http.StatusBadRequest, map[string]string{"error": "unknown offline bundle action"}, nil
			}
		},
	})
	mux.Handle("/", legacy)
	return mux
}
