# Solution Design for Security P1 Remediation

This implementation blueprint details exact technical steps, tools/libraries, sequence, code touchpoints, and verification actions for each milestone from `sec-fixes-01.md`. It is self-contained for direct engineering execution.

---

## 1. Prevent Secrets Exposure
- **Static Audit:** Use regex and code review for logging of env/config/secret patterns. CI job: implement regex scan for tokens/keys in log/error output (e.g., grep `TOKEN|SECRET|API_KEY|Bearer`).
- **Logging Middleware:** Refactor logging (ex: wrap standard logger) so all log/error output passes through a redact function. Maintain allowlist of safe fields; everything else is redacted by default.
- **Error Flow Tests:** Write Go unit/integration tests that simulate panics and forced error paths, capturing output. Assert by regex that no secret patterns appear in output.
- **Documentation:** Add dev doc section with 'never log secrets' guidance. Reference P1.1 in reviews.

## 2. Enforce Strong REST API Authentication
- **Token Criteria:** Define entropy rule (e.g., min 32 chars, min 8-bit randomness per char). Document in config comments and dev docs.
- **Startup Guard:** In Go main/init, fail process at startup if token missing/weak (log fatal: 'API_BEARER_TOKEN too weak or missing').
- **Config Update:** Update all shipped config.json/.env.example: require manual entry, leave example value as `CHANGEME-REQUIRED`.
- **CI Test:** Simulate boot with default/weak values—unit test expects process to exit nonzero.

## 3. Harden CORS Policy
- **Allowlist Only:** Refactor HTTP handler config: production mode (default or non-dev env) refuses `*` and reads from env/API_CORS_ORIGINS (comma-list); only those allowed.
- **Dev Flag:** Add explicit `ALLOW_CORS_ANY` env/config, forcibly false in prod images. Warn/fail if enabled in prod.
- **Runtime Check:** On startup, log effective CORS policy and refuse if risky in prod.
- **Docs:** Document CORS/mode and allowed origin setup in deployment/readme.

## 4. Address Dependency Vulnerabilities
- **CI Vulnerability Scan:** Integrate `govulncheck` (Go 1.18+) and/or Snyk. CI/build job fails on CVE > MEDIUM.
- **Automated Upgrade:** Use `go get -u`, `go mod tidy` after successful scan; periodic scheduled run. Log upgrades.
- **Manual Review:** Engineer reviews/approves upgrades before merging.
- **Report:** Pipeline posts vulnerability report as artifact to PR/MR.

## 5. Ensure Container Identity and File Permissions
- **Non-root Enforcement:** In Containerfile, add `USER 1001:1001` as final layer. Entrypoint check: if UID=0, fail with error.
- **Permissions Audit:** At startup, script checks configs/mounts for 644 (files) & 755 (dirs); log/warn/fail on error.
- **CI Regression Test:** Add pipeline/test that scans built images for UID/GID and file permissions.
- **Docs:** Update ops/K8s/Podman guides for non-root use. Flag mount requirements.

## 6. Secrets Hygiene & Rotation
- **Static Scan:** Use pre-commit hook with truffleHog and regexes for key/token patterns. CI naively blocks if scan fails.
- **Remove Examples:** All sample/configs changed to contain placeholders, never real tokens.
- **Rotation SOP:** Draft secret rotation runbook (markdown) in docs/ with automation hints for major providers.
- **Incident Plan:** Doc escalation/invalidation if token/key found in logs/repo.

## 7. Confirm Enforced REST API Rate Limiting
- **Limiter:** Integrate `golang.org/x/time/rate` or existing rate-limiter (e.g., github.com/ulule/limiter) at HTTP handler. 10/sec, burst 20 per IP/key as default.
- **Prod Requirement:** Refuse API boot/serve without limits set in production.
- **Test Path:** Write stress test in Go (simulate 100+ rps); assert 429 responses and log output.
- **Alerting:** Add/extend logging path to monitoring channel/webhook on violations.

## 8. Validate All Critical Environment Variables
- **Inventory:** List required envs in single `env_requirements.go` (tokens, IDs, URLs, etc). Mark optional vs critical.
- **Startup Guard:** Central function in main.go runs validation; logs all errors, fails-fast unless all critical envs set and correct.
- **Fuzz Testing:** Add CI job intentionally missing, empty, or malformed env values—assert expected failures.
- **Docs:** Update Getting Started/Runbook to cover required env vars and example startup failures.

---

## Implementation Flow & Assignment
- Begin with secret exposure audit and fixing logging (blocker for all further launch).
- In parallel: CORS, token guard, env validation, and container/user hardening.
- Secrets rotation/process/doc and dependency/CI work can run concurrently after first pass.
- Code review and CI gate for every change (unit, integration, doc, example updates).

## Verification
- Completion of each task includes demonstration (unit test, CI run, manual log/code review, and documentation updates).
- No deployment/release allowed with incomplete items or failed audits.

---

## References
- This plan fulfills all `sec-fixes-01.md` milestones.
- Each section references a directly mapped P1 requirement and maps to real Go code, CI config, docs, and containers.

# END OF SOLUTION DESIGN PLAN
