package fake

// globalSearchRelease is when, in the fixed frame, a search-api release
// began to subscribe to catalog.cache.invalidate twice.
var globalSearchRelease = fixed(11, 2, 0)

// globalSubs are example-global's subscriptions by client: reply inboxes,
// three order-service connections in a queue group, the services'
// request subjects in queue groups, search-api subscribed twice to its
// cache invalidations (client-009), inventory-service's four workers per
// connection in one queue group (which client-009 leaves out), and
// notification-worker on a wide subject.
func globalSubs() map[string][]clientSub {
	inbox := clientSub{inbox: true}
	return map[string][]clientSub{
		"api-gateway":       {inbox},
		"order-service":     {inbox, {subject: "orders.place", queue: "order-service", first: 3, rate: 4.2}},
		"checkout-service":  {inbox, {subject: "checkout.quote", queue: "checkout-service", rate: 2.5}},
		"inventory-service": {inbox, {subject: "inventory.reserve", queue: "inventory-service", rate: 0.8, times: 4}},
		"shipping-service":  {{subject: "shipping.rates.request", queue: "shipping-service", rate: 1.5}},
		"search-api": {inbox, {subject: "search.query", queue: "search-api", rate: 6},
			{subject: "catalog.cache.invalidate", rate: 0.4, times: 2, timesFrom: globalSearchRelease}},
		"notification-worker": {{subject: "customer.notifications.>", rate: 3.4}},
	}
}

// prodSubs are example-prod's subscriptions by client.
func prodSubs() map[string][]clientSub {
	inbox := clientSub{inbox: true}
	return map[string][]clientSub{
		"media-worker":     {inbox},
		"order-service":    {inbox, {subject: "orders.place", queue: "order-service"}},
		"analytics-worker": {{subject: "order.archive.>"}},
		"ledger-service":   {{subject: "ledger.post", queue: "ledger-service"}},
		"api-gateway":      {inbox},
		"catalog-api":      {inbox, {subject: "catalog.lookup", queue: "catalog-api"}},
	}
}
