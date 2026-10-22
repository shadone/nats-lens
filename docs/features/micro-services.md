# Micro services

- **Surfaces:** `ui`, `api`
- **Status:** planned
- **Related:** [applications.md](applications.md), [subjects.md](subjects.md), [monitoring-snapshots.md](monitoring-snapshots.md)

## What it does

Planned. Only the collection is built: nats-lens already asks and stores the replies (see [monitoring-snapshots.md](monitoring-snapshots.md)); the page and the API are not built. Services built on the NATS micro framework announce themselves on `$SRV` subjects. nats-lens finds them through `$SRV.INFO`, measures them through `$SRV.STATS` every collect interval, and will show each service with its instances, request rate and errors, and each endpoint with its subject, queue group, average processing time and last error. The scenarios below describe intended behaviour.

## Behavior and rules

- **Discovery.** nats-lens sends `$SRV.INFO` to find the services and their endpoints, in the accounts listed under `accounts`, over each account's own connection.
- **Stats every collection.** One `$SRV.STATS` request per account every collect interval gives each instance's request count, error count, processing time and last error.
- **Services.** One card per service: name and version, description, instances, requests per second, errors and a note.
- **Endpoints.** Per service, one row per endpoint: name, subject, queue group, requests per second, average time, error share and last error. An endpoint whose average time is far above its own 7-day level says so.
- **Missing and mixed.** A service instance that stops answering shows as "instance missing"; versions that differ across instances as "mixed versions".
- **Permissions.** The account user under `accounts` needs to publish to `$SRV.INFO` and `$SRV.STATS` in the services' account and receive the replies on `_INBOX.>`; the README's grants include them.

## Scenarios

### See a slow endpoint

- **Surfaces:** `ui`
- **Given** the service `orders` with the endpoint `get` on `orders.get.*`
- **When** its average time rises to ten times its 7-day level
- **Then** the endpoint row says so, with the time it started

### An instance stops answering

- **Surfaces:** `ui`
- **Given** the service `orders` with three instances
- **When** one instance stops answering `$SRV.STATS`
- **Then** the service shows "instance missing"

### List services from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client asks for the micro services
- **Then** it receives each service with its instances and endpoints

## Not supported / out of scope

- **Only services built on the micro framework.** Request-reply services that do not answer `$SRV` requests are not found.
- **No calls to the endpoints themselves.** nats-lens sends only the framework's discovery and stats requests.
