# Datadog → PagerDuty cross-plugin example

A Datadog Monitor pages a PagerDuty Service when it triggers, using
Datadog's `@pagerduty-<service-name>` notification syntax.

## Topology

```
  Datadog Monitor
        │  message contains @pagerduty-Checkout-API
        ▼
  Datadog PagerDuty integration   ◀── OAuth handshake (Datadog UI, one-time) ──▶  PagerDuty
        │
        ▼
  PagerDuty Service "Checkout API"
        │
        ▼
  Escalation Policy → Schedule → User
```

## How this differs from the Grafana example

Datadog's PagerDuty integration is **OAuth-based**, not routing-key-based.
You install the integration once via Datadog's UI (Integrations →
PagerDuty → Configure → Authorize). After that, any Datadog Monitor whose
`message` contains `@pagerduty-<service-name>` routes incidents to the
matching PagerDuty Service - `<service-name>` is the PD service name with
spaces normalized to hyphens.

That means **no integration key Resolvable is needed**. Instead, the cross-
plugin wiring is a Pkl-time string share: a local `serviceName` constant
is read by both the PD Service's `name` and the Datadog Monitor's
`@pagerduty-<handle>` mention, so the two stay in sync at evaluation time.
Renaming the service in one place automatically updates the other.

If you'd rather use routing keys directly, prefer
[`../grafana-integration/`](../grafana-integration/) for the canonical
single-apply pattern.

## One-time setup

1. In Datadog: Integrations → PagerDuty → click **Configure**.
2. Authorize Datadog to talk to your PagerDuty account.
3. Confirm the service name "Checkout API" appears in Datadog's list of
   PagerDuty services after the first apply.

> Only OAuth-installed integration routes `@pagerduty-` mentions.
> `POST /api/v1/integration/pagerduty` looks equivalent but doesn't.

## Run

```bash
source .env                                  # PAGERDUTY_TOKEN
export DD_API_KEY=<datadog-api-key>          # for the PD plugin nothing needed
export DD_APP_KEY=<datadog-app-key>
export DD_SITE=datadoghq.com                 # or your site

formae apply --mode reconcile examples/datadog-integration/main.pkl
formae inventory
formae destroy examples/datadog-integration/main.pkl
```

## Verifying

After apply:

- PagerDuty: the Service "Checkout API" exists, attached to the demo
  Escalation Policy.
- Datadog: the Monitor exists. Resolve a synthetic incident in the
  Datadog UI to see PagerDuty receive a page.
