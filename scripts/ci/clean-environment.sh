#!/bin/bash
# © 2026 Platform Engineering Labs Inc.
# SPDX-License-Identifier: FSL-1.1-ALv2
#
# Clean PagerDuty test resources created by conformance tests.
# Invoked before AND after the conformance run. Failures are non-fatal.

set -uo pipefail

TEST_PREFIX="${TEST_PREFIX:-formae-conformance-}"

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

# purge <api-path> <json-collection-key> [match-email]
#
# Lists resources whose name (or, when match-email is "email", email) begins
# with TEST_PREFIX and deletes them. Integrations are not purged directly: they
# cascade with their parent Service. Order of the calls below matters — a
# resource cannot be deleted while another still references it (e.g. an
# escalation policy in use by a service), so dependants are purged first.
purge() {
  local path="$1" key="$2" match_email="${3:-}"
  local ids
  ids=$(curl_pd "https://api.pagerduty.com/${path}?limit=100&query=${TEST_PREFIX}" 2>/dev/null \
        | TEST_PREFIX="${TEST_PREFIX}" MATCH_EMAIL="${match_email}" KEY="${key}" python3 -c "
import sys, os, json
prefix = os.environ['TEST_PREFIX']
key = os.environ['KEY']
match_email = os.environ['MATCH_EMAIL'] == 'email'
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
out = []
for r in d.get(key, []):
    if r.get('name', '').startswith(prefix) or (match_email and r.get('email', '').startswith(prefix)):
        out.append(r['id'])
print(' '.join(out))
" 2>/dev/null || true)
  for id in $ids; do
    echo "  deleting ${key%s} $id"
    curl_pd -X DELETE -o /dev/null "https://api.pagerduty.com/${path}/$id" || true
  done
}

# Dependants first, dependencies last.
purge "services" "services"
purge "escalation_policies" "escalation_policies"
purge "schedules" "schedules"
purge "teams" "teams"
purge "users" "users" "email"

echo "clean-environment.sh: done"
exit 0
