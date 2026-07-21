# Changelog

All notable changes to the formae PagerDuty plugin are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Install with `sudo formae plugin install pagerduty` on the host that runs the
formae agent.

## [0.1.2]

### Added

- Initial release of the PagerDuty plugin as a standalone package built on the
  formae Plugin SDK.
- Covers eleven resources across the on-call lifecycle: users, contact methods,
  notification rules, teams, team memberships, schedules, schedule overrides,
  escalation policies, services, integrations, and maintenance windows.
