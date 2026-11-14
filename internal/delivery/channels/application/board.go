package application

import (
	"sort"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/channels/domain"
)

type releaseRef struct {
	product  string
	version  string
	channels []string
}

type promotionRef struct {
	product string
	version string
	from    string
	to      string
	passed  bool
}

type entityRef struct {
	product string
	version string
	channel string
}

type environmentRef struct {
	id             string
	defaultChannel string
	releases       []releaseRef
	promotions     []promotionRef
	entities       []entityRef
}

func boardFrom(environments []environmentRef, observed map[string]string) domain.View {
	view := domain.Empty()
	for _, environment := range environments {
		if environment.id == "" {
			continue
		}
		promotions := catalogPromotions(environment.promotions)
		subscribed := domain.SubscriptionOf(environment.defaultChannel)
		products := map[string]struct{}{}
		for _, release := range environment.releases {
			if release.product != "" {
				products[release.product] = struct{}{}
			}
		}
		for _, entity := range environment.entities {
			if entity.product != "" {
				products[entity.product] = struct{}{}
			}
		}
		names := make([]string, 0, len(products))
		for name := range products {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, product := range names {
			versions := map[string]string{}
			for _, subscription := range view.Channels {
				versions[subscription] = highestOnChannel(environment.releases, promotions, product, domain.CatalogChannel(subscription))
			}
			running := observed[environment.id+"\x00"+product]
			if running == "" {
				running = entityVersion(environment.entities, product, subscribed)
			}
			if running == "" {
				running = versions[subscribed]
			}
			view.Rows = append(view.Rows, domain.Row{
				Environment: environment.id, Product: product, Subscribed: subscribed,
				Versions: versions, Running: running,
			})
		}
	}
	sort.Slice(view.Rows, func(i, j int) bool {
		if view.Rows[i].Environment == view.Rows[j].Environment {
			return view.Rows[i].Product < view.Rows[j].Product
		}
		return view.Rows[i].Environment < view.Rows[j].Environment
	})
	return view
}

func environmentFromPlan(plan controlplane.PlanRecord) (environmentRef, bool) {
	if plan.DesiredState == nil || plan.Plan.Environment == "" {
		return environmentRef{}, false
	}
	state := plan.DesiredState
	environment := environmentRef{id: plan.Plan.Environment, defaultChannel: state.Environment.DefaultReleaseChannel}
	for _, release := range state.Releases {
		environment.releases = append(environment.releases, releaseRef{
			product: release.Product, version: release.Version, channels: append([]string(nil), release.Channels...),
		})
	}
	for _, promotion := range state.Promotions {
		environment.promotions = append(environment.promotions, promotionRef{
			product: promotion.Product, version: promotion.Version,
			from: promotion.FromChannel, to: promotion.ToChannel,
			passed: promotion.Status == controlplane.PromotionPassed,
		})
	}
	for _, entity := range state.Entities {
		environment.entities = append(environment.entities, entityRef{
			product: entity.Product, version: entity.Version, channel: entity.ReleaseChannel,
		})
	}
	return environment, true
}

func highestOnChannel(releases []releaseRef, promotions []catalog.Promotion, product, channel string) string {
	best := ""
	for _, release := range releases {
		if release.product != product || release.version == "" {
			continue
		}
		catalogRelease := catalog.Release{Product: release.product, Version: release.version, Channels: append([]string(nil), release.channels...)}
		if !catalog.ReleaseOnChannel(catalogRelease, promotions, channel) {
			continue
		}
		if best == "" || catalog.CompareVersions(release.version, best) > 0 {
			best = release.version
		}
	}
	return best
}

func entityVersion(entities []entityRef, product, subscribed string) string {
	for _, entity := range entities {
		if entity.product == product && domain.SubscriptionOf(entity.channel) == subscribed && subscribed != "" {
			return entity.version
		}
	}
	return ""
}

func catalogPromotions(promotions []promotionRef) []catalog.Promotion {
	out := make([]catalog.Promotion, 0, len(promotions))
	for _, promotion := range promotions {
		out = append(out, catalog.Promotion{
			Product: promotion.product, Version: promotion.version,
			FromChannel: promotion.from, ToChannel: promotion.to, Passed: promotion.passed,
		})
	}
	return out
}
