# PagerDuty Plugin for Formae

[![CI](https://github.com/platform-engineering-labs/formae-plugin-pagerduty/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/platform-engineering-labs/formae-plugin-pagerduty/actions/workflows/ci.yml)
[![Nightly](https://github.com/platform-engineering-labs/formae-plugin-pagerduty/actions/workflows/nightly.yml/badge.svg?branch=main)](https://github.com/platform-engineering-labs/formae-plugin-pagerduty/actions/workflows/nightly.yml)

PagerDuty resource plugin for [formae](https://formae.io). Manages on-call infrastructure - users, teams, schedules, escalation policies, services, and the paging primitives around them - as code via the PagerDuty REST API.

## Supported resources

| Resource type | Description |
|---|---|
| `PAGERDUTY::Core::User` | PagerDuty user account. Identified by email; full CRUD. |
| `PAGERDUTY::Core::ContactMethod` | A user's email / phone / SMS channel. |
| `PAGERDUTY::Core::NotificationRule` | Pages one of a user's contact methods at a given urgency, after a start delay. |
| `PAGERDUTY::Core::Team` | Logical user grouping. |
| `PAGERDUTY::Core::TeamMembership` | Adds a user to a team with a role (observer / responder / manager). |
| `PAGERDUTY::Core::Schedule` | On-call rotation with polymorphic layer restrictions (daily / weekly). |
| `PAGERDUTY::Core::ScheduleOverride` | Temporary on-call coverage for a window (vacation / swaps). Immutable - any change replaces. |
| `PAGERDUTY::Core::EscalationPolicy` | Ordered escalation rules with discriminated targets (user / schedule). |
| `PAGERDUTY::Core::Service` | Alert routing endpoint referencing an escalation policy. |
| `PAGERDUTY::Core::MaintenanceWindow` | Silences one or more services for a time range (e.g. during a deploy). |
| `PAGERDUTY::Core::Integration` | Service-scoped event integration. Exposes `integrationKey` as a Resolvable so observability plugins (Grafana, Datadog, CloudWatch via SNS) can wire alert sinks to a PagerDuty Service in code. |

## Target configuration

```pkl
import "@pagerduty/pagerduty.pkl" as pd

new formae.Target {
  label = "pagerduty"
  namespace = "PAGERDUTY"
  config = new pd.Config {
    subdomain = "your-subdomain"   // immutable; identifies the PD account
    fromEmail = "oncall@your-domain.com"  // optional, used as From: header on endpoints that require it
  }
}
```

The Target Config carries **no credentials** - it only identifies which PagerDuty account this Target represents. The API token is resolved at operation time.

## Credentials

The plugin resolves the PagerDuty API token via a chain, in order:

1. `PAGERDUTY_TOKEN` environment variable
2. `~/.config/pagerduty/token` (single-line file)

Create a General Access REST API key in your PagerDuty account: **Integrations > API Access Keys > Create New API Key**. Leave "Read-only" unchecked.

Local development:

```bash
cp .env.example .env
# edit .env, set PAGERDUTY_TOKEN
source .env
```

**The token is never persisted to the formae datastore** - it's read fresh on each client construction and tagged `json:"-"` on the parsed Target Config struct so it cannot accidentally leak into Target Config serialization. (See `pkg/config/config.go`.)

> **Limitation:** because credentials live in process state, a single formae agent can only talk to one PagerDuty account at a time. Per-Target tokens depend on the upstream "opaque-on-Target-Config" SDK feature, tracked separately.

## Examples

- [`examples/users-and-teams/main.pkl`](examples/users-and-teams/main.pkl) - Minimal starting point: a User and a Team.
- [`examples/schedule-restrictions/main.pkl`](examples/schedule-restrictions/main.pkl) - Multi-layer schedule with both `daily_restriction` and `weekly_restriction` to exercise the polymorphic Restriction sub-resource.
- [`examples/grafana-integration/`](examples/grafana-integration/) - Cross-plugin demo: Grafana ContactPoint paging a PagerDuty Service via the Integration resource's `integrationKey` Resolvable.
- [`examples/datadog-integration/`](examples/datadog-integration/) - Cross-plugin demo: Datadog Monitor paging a PagerDuty Service via the OAuth-based `@pagerduty-<service-name>` mention pattern.

```bash
source .env
formae apply --mode reconcile examples/users-and-teams/main.pkl
formae inventory
formae destroy examples/users-and-teams/main.pkl
```

## Testing

```bash
# Unit (config + token resolution chain)
make test

# Integration (real PagerDuty API - requires PAGERDUTY_TOKEN)
source .env
make test-integration

# Conformance (full plugin lifecycle through formae)
make install
source .env
make conformance-test
```

The integration suite creates and destroys test users / teams / schedules / policies / services in the configured PagerDuty account. Test resources are named `formae-pd-test-*` and `formae-conformance-*`; the cleanup script (`scripts/ci/clean-environment.sh`) removes orphans by name prefix.

### Sandbox-account note

The PagerDuty account under test must allow the configured email domain for user creation. By default, tests use `@platform.engineering`; override with `PAGERDUTY_TEST_DOMAIN=<your-domain>` if your sandbox enforces a different allow-list.

## License

FSL-1.1-ALv2.
