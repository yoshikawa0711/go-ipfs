#!/usr/bin/env bash

test_description="Test CORS behavior on HTTP ports (RPC API and Gateway)"

. lib/test-lib.sh

test_init_ipfs

# Default config

test_expect_success "Default API.HTTPHeaders config is empty" '
    echo "{}" > expected &&
    ipfs config --json API.HTTPHeaders > actual &&
    test_cmp expected actual
'

test_expect_success "Default Gateway.HTTPHeaders config match expected values" '
cat <<EOF > expected
{
  "Access-Control-Allow-Headers": [
    "X-Requested-With",
    "Range",
    "User-Agent"
  ],
  "Access-Control-Allow-Methods": [
    "GET"
  ],
  "Access-Control-Allow-Origin": [
    "*"
  ]
}
EOF
    ipfs config --json Gateway.HTTPHeaders > actual &&
    test_cmp expected actual
'

test_launch_ipfs_daemon

thash='QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn'

# Gateway

# HTTP GET Request
test_expect_success "GET to Gateway succeeds" '
  curl -svX GET "http://127.0.0.1:$GWAY_PORT/ipfs/$thash" >/dev/null 2>curl_output &&
  cat curl_output
'

# GET Response from Gateway should contain CORS headers
test_expect_success "GET response for Gateway resource looks good" '
  grep "< Access-Control-Allow-Origin: \*" curl_output &&
  grep "< Access-Control-Allow-Methods: GET" curl_output &&
  grep "< Access-Control-Allow-Headers: Range" curl_output &&
  grep "< Access-Control-Expose-Headers: Content-Range" curl_output
'

# HTTP OPTIONS Request
test_expect_success "OPTIONS to Gateway succeeds" '
  curl -svX OPTIONS -H "Origin: https://example.com" "http://127.0.0.1:$GWAY_PORT/ipfs/$thash" 2>curl_output &&
  cat curl_output
'

# OPTION Response from Gateway should contain CORS headers
test_expect_success "OPTIONS response for Gateway resource looks good" '
  grep "< Access-Control-Allow-Origin: \*" curl_output &&
  grep "< Access-Control-Allow-Methods: GET" curl_output &&
  grep "< Access-Control-Allow-Headers: Range" curl_output &&
  grep "< Access-Control-Expose-Headers: Content-Range" curl_output
'

test_kill_ipfs_daemon

# Test CORS safelisting of custom headers
test_expect_success "Can configure gateway headers" '
  ipfs config --json Gateway.HTTPHeaders.Access-Control-Allow-Headers "[\"X-Custom1\"]" &&
  ipfs config --json Gateway.HTTPHeaders.Access-Control-Expose-Headers "[\"X-Custom2\"]" &&
  ipfs config --json Gateway.HTTPHeaders.Access-Control-Allow-Origin "[\"localhost\"]"
'

test_launch_ipfs_daemon

test_expect_success "OPTIONS to Gateway without custom headers succeeds" '
  curl -svX OPTIONS -H "Origin: https://example.com" "http://127.0.0.1:$GWAY_PORT/ipfs/$thash" 2>curl_output &&
  cat curl_output
'
# Range and Content-Range are safelisted by default, and keeping them makes better devexp
# because it does not cause regressions in range requests made by JS
test_expect_success "Access-Control-Allow-Headers extends the implicit list" '
  grep "< Access-Control-Allow-Headers: Range" curl_output &&
  grep "< Access-Control-Allow-Headers: X-Custom1" curl_output &&
  grep "< Access-Control-Expose-Headers: Content-Range" curl_output &&
  grep "< Access-Control-Expose-Headers: X-Custom2" curl_output
'

test_expect_success "OPTIONS to Gateway with a custom header succeeds" '
  curl -svX OPTIONS -H "Origin: https://example.com" -H "Access-Control-Request-Headers: X-Unexpected-Custom" "http://127.0.0.1:$GWAY_PORT/ipfs/$thash" 2>curl_output &&
  cat curl_output
'
test_expect_success "Access-Control-Allow-Headers extends the implicit list" '
  test_expect_code 1 grep "< Access-Control-Allow-Headers: X-Unexpected-Custom" curl_output &&
  grep "< Access-Control-Allow-Headers: Range" curl_output &&
  grep "< Access-Control-Allow-Headers: X-Custom1" curl_output &&
  grep "< Access-Control-Expose-Headers: Content-Range" curl_output &&
  grep "< Access-Control-Expose-Headers: X-Custom2" curl_output
'

# Origin is sensitive security perimeter, and we assume override should remove
# any implicit records
test_expect_success "Access-Control-Allow-Origin replaces the implicit list" '
  grep "< Access-Control-Allow-Origin: localhost" curl_output
'

# Read-Only /api/v0 RPC API (exposed on the Gateway Port)

# HTTP GET Request
test_expect_success "GET to {gw}/api/v0 succeeds" '
  curl -svX GET "http://127.0.0.1:$GWAY_PORT/api/v0/cat?arg=$thash" >/dev/null 2>curl_output
'
# GET Response from the API should NOT contain CORS headers
# Blacklisting: https://git.io/vzaj2
# Rationale: https://git.io/vzajX
test_expect_success "GET response from {gw}/api/v0 has no CORS headers" '
  grep -q "Access-Control-Allow-" curl_output && false || true
'

# HTTP OPTIONS Request
test_expect_success "OPTIONS to {gw}/api/v0 succeeds" '
  curl -svX OPTIONS -H "Origin: https://example.com" "http://127.0.0.1:$GWAY_PORT/api/v0/cat?arg=$thash" 2>curl_output
'
# OPTIONS Response from the API should NOT contain CORS headers
test_expect_success "OPTIONS response from {gw}/api/v0 has no CORS header" '
  cat curl_output &&
  grep -q "Access-Control-Allow-" curl_output && false || true
'

test_kill_ipfs_daemon

test_done
