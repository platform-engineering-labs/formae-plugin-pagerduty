#!/bin/bash
# © 2026 Platform Engineering Labs Inc.
# SPDX-License-Identifier: FSL-1.1-ALv2
#
# Clean PagerDuty test resources whose name/email/description begins with
# TEST_PREFIX (default "formae-", which covers both conformance
# "formae-conformance-" and integration "formae-test-" resources; real resources
# are never named "formae-"). Safe to run before AND after a test run; failures
# are non-fatal. Each type is purged in a loop so more than one page of leftovers
# and cascade-delayed children are cleared, paced to stay under the rate limit.

set -uo pipefail

TEST_PREFIX="${TEST_PREFIX:-formae-}"

if [ -z "${PAGERDUTY_TOKEN:-}" ]; then
  echo "clean-environment.sh: PAGERDUTY_TOKEN not set; skipping cleanup"
  exit 0
fi

echo "clean-environment.sh: cleaning PagerDuty resources with prefix '${TEST_PREFIX}'"

curl_pd() {
  curl -s -H "Authorization: Token token=${PAGERDUTY_TOKEN}" \
       -H "Accept: application/vnd.pagerduty+json;version=2" \
       -H "Content-Type: application/json" \
       "$@"
}

# purge <api-path> <json-collection-key> [match-field]
#
# Lists resources whose <match-field> (name, email, or description; default
# name) begins with TEST_PREFIX and deletes them, repeating until none remain so
# that more than 100 leftovers and cascade-delayed children are cleared.
# Integrations are not purged directly: they cascade with their parent Service.
# Order of the calls below matters - a resource cannot be deleted while another
# still references it (e.g. a service using an escalation policy), so dependants
# are purged before their dependencies.
purge() {
  local path="$1" key="$2" field="${3:-name}"
  local round ids id
  for round in 1 2 3 4 5; do
    ids=$(curl_pd "https://api.pagerduty.com/${path}?limit=100" 2>/dev/null \
          | TEST_PREFIX="${TEST_PREFIX}" KEY="${key}" FIELD="${field}" python3 -c "
import sys, os, json
prefix = os.environ['TEST_PREFIX']
key = os.environ['KEY']
field = os.environ['FIELD']
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
out = [r['id'] for r in d.get(key, []) if (r.get(field) or '').startswith(prefix)]
print(' '.join(out))
" 2>/dev/null || true)
    [ -z "${ids}" ] && break
    for id in ${ids}; do
      echo "  deleting ${key%s} ${id}"
      curl_pd -X DELETE -o /dev/null "https://api.pagerduty.com/${path}/${id}" || true
      sleep 0.3   # pace below PagerDuty's account-wide rate limit
    done
  done
}

# Dependants first, dependencies last.
purge "maintenance_windows" "maintenance_windows" "description"
purge "services" "services"
purge "escalation_policies" "escalation_policies"
purge "schedules" "schedules"
purge "teams" "teams"
purge "users" "users" "email"

echo "clean-environment.sh: done"
exit 0
