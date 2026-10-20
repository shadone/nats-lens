-- nats-server counts each consumer against the tier of its own replicas,
-- so one stream can hold consumers of every tier; the comment of 0029 said
-- the tiers' consumer limits combine as the largest.

COMMENT ON COLUMN account_details.limit_js_consumers IS 'Most consumers one stream may hold under the account JWT (nats-server applies the limit per stream); -1 no limit, also when the JWT says 0; from the tiers the sum, since each consumer counts against the tier of its own replicas, -1 when any tier has no limit or the sum overflows; NULL without a JWT.';
