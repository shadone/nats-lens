package fake

import "time"

// microID is a stable instance ID of 22 characters, as the framework's
// look.
func microID(seed string) string {
	t := token(seed)
	return t + token(t)[:10]
}

// globalMicro are example-global's micro services, in APP, which nats-lens
// asks: orders, whose get endpoint has taken ten times its usual time since
// 08:12 and whose create endpoint last failed on a duplicate order;
// pricing, three instances on 2.0.0 (one restarted in-process at 14:21) and
// one on 1.9.3; inventory, healthy, though one instance hung from 10:05
// to 10:47 and was missing then.
func globalMicro() []microSpec {
	deployed := time.Date(2026, 10, 15, 7, 30, 0, 0, time.UTC)
	pricing := time.Date(2026, 10, 16, 18, 5, 0, 0, time.UTC)
	restart := fixed(14, 21, 10)
	return []microSpec{
		{name: "orders", description: "Order intake and lookup", client: "order-service",
			instances: []microInstanceSpec{
				{id: microID("orders/0"), version: "1.4.2", server: "nats-eu-0", started: deployed},
				{id: microID("orders/1"), version: "1.4.2", server: "nats-eu-1", started: deployed.Add(40 * time.Second)},
				{id: microID("orders/2"), version: "1.4.2", server: "nats-eu-2", started: deployed.Add(85 * time.Second)},
			},
			endpoints: []microEndpointSpec{
				{name: "create", subject: "orders.create", rate: 42.1, ms: 3.8, errors: 0.001, lastError: "409 duplicate order id", lastErrorBy: 1},
				{name: "get", subject: "orders.get.*", rate: 118.4, ms: 4.1, slowFrom: fixed(8, 11, 56), slowBy: 10},
				{name: "cancel", subject: "orders.cancel", rate: 1.2, ms: 5.1},
			}},
		{name: "pricing", description: "Price quotes", client: "checkout-service",
			instances: []microInstanceSpec{
				{id: microID("pricing/0"), version: "2.0.0", server: "nats-eu-0", conn: 1, started: pricing},
				{id: microID("pricing/1"), version: "2.0.0", server: "nats-eu-1", conn: 1, started: pricing.Add(30 * time.Second)},
				{id: microID("pricing/2"), version: "2.0.0", server: "nats-eu-2", conn: 1, started: pricing.Add(time.Minute), to: restart},
				{id: microID("pricing/2b"), version: "2.0.0", server: "nats-eu-2", conn: 1, from: restart},
				{id: microID("pricing/3"), version: "1.9.3", server: "nats-eu-3", conn: 1, started: time.Date(2026, 10, 12, 9, 40, 0, 0, time.UTC)},
			},
			endpoints: []microEndpointSpec{
				{name: "quote", subject: "pricing.quote", rate: 80.5, ms: 2.1, errors: 0.0004},
				{name: "rules", subject: "pricing.rules.get", rate: 7.5, ms: 0.9},
			}},
		{name: "inventory", description: "Stock levels", client: "inventory-service",
			instances: []microInstanceSpec{
				{id: microID("inventory/0"), version: "0.9.1", server: "nats-eu-4", started: deployed.Add(3 * time.Hour)},
				{id: microID("inventory/1"), version: "0.9.1", server: "nats-eu-5", started: deployed.Add(3 * time.Hour),
					hungFrom: fixed(10, 5, 10), hungTo: fixed(10, 47, 40)},
			},
			endpoints: []microEndpointSpec{
				{name: "stock", subject: "inventory.stock.get", rate: 11.4, ms: 1.6},
				{name: "reserve", subject: "inventory.stock.reserve", rate: 1.2, ms: 6.2},
			}},
	}
}

// prodMicro are example-prod's micro services. nats-lens does not ask its
// account, so they are known from their $SRV subscriptions only.
func prodMicro() []microSpec {
	started := time.Date(2026, 10, 14, 11, 0, 0, 0, time.UTC)
	return []microSpec{
		{name: "catalog", description: "Product catalog", client: "catalog-api",
			instances: []microInstanceSpec{
				{id: microID("catalog/0"), version: "3.2.0", server: "nats-0", started: started},
				{id: microID("catalog/1"), version: "3.2.0", server: "nats-1", started: started},
			},
			endpoints: []microEndpointSpec{{name: "item", subject: "catalog.items.get", rate: 21, ms: 2.4}}},
		{name: "ledger", description: "Account balances", client: "ledger-service",
			instances: []microInstanceSpec{
				{id: microID("ledger/0"), version: "1.1.0", server: "nats-1", started: started},
				{id: microID("ledger/1"), version: "1.1.0", server: "nats-2", started: started},
			},
			endpoints: []microEndpointSpec{{name: "balance", subject: "ledger.balance.get", rate: 3.5, ms: 7}}},
	}
}
