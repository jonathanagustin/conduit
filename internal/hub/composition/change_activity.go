package composition

import (
	"context"
	"errors"
	activityhttp "github.com/jonathanagustin/conduit/internal/changeactivity/adapters/httpapi"
	activityapp "github.com/jonathanagustin/conduit/internal/changeactivity/application"
	activity "github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	activityports "github.com/jonathanagustin/conduit/internal/changeactivity/ports"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	notificationhttp "github.com/jonathanagustin/conduit/internal/notifications/adapters/httpapi"
	"net/http"
)

func mountChangeActivity(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, store activityports.Store) (http.Handler, error) {
	service := controlplane.NewGovernanceService(repository)
	bundles, err := controlplane.NewBundleService(repository, options.BundlePrivateKey, options.TrustedBundleKeys)
	if err != nil {
		return nil, err
	}
	config := activityhttp.Config{BindLabels: func(r *http.Request) (activityhttp.LabelMutator, error) {
		if _, err := activityCarrier(r.Context()); err != nil {
			return nil, err
		}
		return activityLabels{governance: service, bundles: bundles}, nil
	}}
	if store != nil {
		config.BindDiscussion = func(r *http.Request) (*activityapp.Service, error) {
			if _, err := activityCarrier(r.Context()); err != nil {
				return nil, err
			}
			return activityapp.New(store, activityAuthority{governance: service, options: options})
		}
	}
	mux := http.NewServeMux()
	if err := activityhttp.Register(mux, config); err != nil {
		return nil, err
	}
	mux.Handle("/", legacy)
	return withOperatorCarrier(service, options, mux), nil
}
func activityCarrier(ctx context.Context) (governance.Principal, error) {
	carrier, err := operatorFrom(ctx)
	if err == nil {
		return carrier, nil
	}
	status := http.StatusUnauthorized
	var identity notificationhttp.StatusError
	if errors.As(err, &identity) {
		status = identity.Status
	}
	return governance.Principal{}, activityhttp.StatusError{Status: status, Message: "operator identity could not be established"}
}
func activityError(err error) error {
	switch {
	case errors.Is(err, controlplane.ErrForbidden):
		return activity.ErrForbidden
	case errors.Is(err, controlplane.ErrNotFound):
		return activity.ErrNotFound
	case errors.Is(err, controlplane.ErrConflict), errors.Is(err, controlplane.ErrInvalidTransition):
		return activity.ErrConflict
	}
	return err
}

type activityAuthority struct {
	governance *controlplane.GovernanceService
	options    controlplane.HandlerOptions
}

func (a activityAuthority) RequireAnnotation(ctx context.Context, id string) (activityports.Writer, error) {
	carrier, err := activityCarrier(ctx)
	if err != nil {
		return activityports.Writer{}, err
	}
	if _, err := a.governance.RequireChangeRequestAnnotation(ctx, id, carrier); err != nil {
		return activityports.Writer{}, activityError(err)
	}
	return activityports.Writer{Author: governance.EffectivePrincipal(carrier).Username, Actor: carrier.Username}, nil
}
func (a activityAuthority) RequireRead(ctx context.Context, id string) error {
	carrier, err := activityCarrier(ctx)
	if err != nil {
		return err
	}
	_, err = a.governance.GetReadableChangeRequest(ctx, id, carrier, a.options)
	return activityError(err)
}

type activityLabels struct {
	governance *controlplane.GovernanceService
	bundles    *controlplane.BundleService
}

func (a activityLabels) MutateLabel(ctx context.Context, id, name, operation string, dryRun bool) (activity.LabelMutation, error) {
	carrier, err := activityCarrier(ctx)
	if err != nil {
		return activity.LabelMutation{}, err
	}
	request, plan, approved, err := a.governance.MutateChangeRequestLabel(ctx, id, carrier, name, operation, dryRun)
	if err != nil {
		return activity.LabelMutation{}, activityError(err)
	}
	mode := "PREVIEW"
	if !dryRun {
		mode = "APPLIED"
		if _, err := a.bundles.FinalizeApprovedChangeRequest(ctx, request, approved); err != nil {
			return activity.LabelMutation{}, activityError(err)
		}
	}
	return activity.LabelMutation{ChangeRequestID: request.ID, Labels: append([]string{}, request.Labels...), ResultingStatus: request.Status, PlanID: request.PlanID, PlanStatus: string(plan.Status), ApprovalTransition: approved, Mode: mode, ValueTier: activity.ValueTier}, nil
}
