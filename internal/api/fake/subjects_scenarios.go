package fake

// globalSubs are example-global's subscriptions by client: reply inboxes,
// three order-service connections in a queue group, the services'
// request subjects in queue groups, and notification-worker on a wide
// subject.
func globalSubs() map[string][]clientSub {
	inbox := clientSub{inbox: true}
	return map[string][]clientSub{
		"api-gateway":         {inbox},
		"order-service":       {inbox, {subject: "orders.place", queue: "order-service", first: 3}},
		"checkout-service":    {inbox, {subject: "checkout.quote", queue: "checkout-service"}},
		"inventory-service":   {inbox},
		"shipping-service":    {{subject: "shipping.rates.request", queue: "shipping-service"}},
		"search-api":          {inbox, {subject: "search.query", queue: "search-api"}},
		"notification-worker": {{subject: "customer.notifications.>"}},
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
