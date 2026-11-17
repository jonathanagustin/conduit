package main

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/hub/composition"
	notificationspostgres "github.com/jonathanagustin/conduit/internal/notifications/adapters/postgres"
	notificationdomain "github.com/jonathanagustin/conduit/internal/notifications/domain"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
	metricsapplication "github.com/jonathanagustin/conduit/internal/observability/application"
	metricsdomain "github.com/jonathanagustin/conduit/internal/observability/domain"
)

// metricsGather is the shape every layer of the metrics registry has: the
// queue gauges at the bottom, and one wrapper per thing that adds series.
type metricsGather = func(context.Context) (*metricsdomain.Registry, error)

// metricsExtension adds one subsystem's series to a gather. Nil adds nothing.
type metricsExtension func(metricsGather) metricsGather

// requestMetrics publishes the Hub's request counters.
func requestMetrics(recorder *metricsapplication.HTTPRecorder) metricsExtension {
	return func(gather metricsGather) metricsGather {
		return metricsapplication.WithHTTPRequests(gather, recorder)
	}
}

// notificationDeliveryMetrics publishes notification delivery: this replica's
// attempt counters, and the queue's backlog read from the database, which is
// the same from whichever replica is scraped.
//
// It returns nil when no transport is configured. A Hub that delivers nothing
// still queues inbox rows that would be due, and exporting those as a growing
// backlog would page somebody about a feature that is switched off.
func notificationDeliveryMetrics(
	store *notificationspostgres.Store, transport notificationports.Transport,
	recorder *metricsapplication.DeliveryRecorder,
) metricsExtension {
	if store == nil || transport == nil {
		return nil
	}
	read := func(ctx context.Context) ([]metricsapplication.DeliveryBacklog, error) {
		rows, err := store.DeliveryBacklog(ctx, time.Now())
		if err != nil {
			return nil, err
		}
		backlog := make([]metricsapplication.DeliveryBacklog, 0, len(rows))
		for _, row := range rows {
			backlog = append(backlog, metricsapplication.DeliveryBacklog{
				// The channel becomes a kind of transport here, and only a
				// kind: nothing else about the row is read.
				Transport: composition.NotificationTransportKind(transport, notificationdomain.Channel(row.Channel)),
				Pending:   int(row.Pending), Exhausted: int(row.Exhausted), OldestPendingAge: row.OldestPendingAge,
			})
		}
		return backlog, nil
	}
	return func(gather metricsGather) metricsGather {
		return metricsapplication.WithNotificationDeliveries(gather, recorder, read)
	}
}
