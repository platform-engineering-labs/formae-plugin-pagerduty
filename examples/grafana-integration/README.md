# Grafana → PagerDuty cross-plugin example

Wires a Grafana ContactPoint to a PagerDuty Service Integration in a single
apply, so Grafana alerts page on-call rotations defined in PagerDuty.

## Topology

```
  Grafana AlertRule
        │
        ▼
  Grafana ContactPoint (type=pagerduty)
        │  integrationKey  (cross-plugin Resolvable)
        ▼
  PagerDuty Integration  ◀───  belongs to ───  PagerDuty Service
                                                       │
                                                       ▼
                                          Escalation Policy → Schedule → User
```

## Run

```bash
source .env   # PAGERDUTY_TOKEN
export GRAFANA_AUTH=admin:admin
export GRAFANA_URL=https://your-grafana

formae apply --mode reconcile examples/grafana-integration/main.pkl
formae inventory
formae destroy examples/grafana-integration/main.pkl
```

## How it works

The Grafana ContactPoint's `settingsMap` field accepts per-value formae
Resolvables, so the PagerDuty integration key flows straight from the
`Integration.res.integrationKey` Resolvable into the Grafana ContactPoint at
apply time:

```pkl
new ContactPoint.ContactPoint {
  contactPointType = "pagerduty"
  settingsMap = new Mapping {
    ["integrationKey"] = demoIntegration.res.integrationKey
  }
}
```

This requires `formae-plugin-grafana` v0.1.4+ (`ContactPoint.settingsMap`
added) and `formae-plugin-pagerduty` v0.1.0+ (`Integration` resource with
`integrationKey` Resolvable).
