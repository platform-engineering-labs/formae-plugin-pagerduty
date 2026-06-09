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

# Clean test users matching the prefix.
USERS=$(curl_pd "https://api.pagerduty.com/users?limit=100&query=${TEST_PREFIX}" 2>/dev/null \
        | python3 -c "import sys,json; d=json.load(sys.stdin); print(' '.join(u['id'] for u in d.get('users', []) if u.get('name','').startswith('${TEST_PREFIX}') or u.get('email','').startswith('${TEST_PREFIX}')))" 2>/dev/null || true)

for id in $USERS; do
  echo "  deleting user $id"
  curl_pd -X DELETE -o /dev/null "https://api.pagerduty.com/users/$id" || true
done

echo "clean-environment.sh: done"
exit 0
