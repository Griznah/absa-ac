# Security P1 Remediation Implementation Plan

## Scope
This document captures the actionable, milestone-based plan to address all P1 security risks identified in `plans/sec-review-01.md` for the absa-sec-review project. Each item is mapped to explicit requirements, owners, testing, and documentation needs. 

---

## 1. Prevent Secrets Exposure
**Objective:** No code path ever exposes Discord or API tokens outside secure process memory.

**Milestones:**
- [ ] Audit logs, errors, debugging, panics; find all secret outputs (dev + prod).
- [ ] Implement structured logging middleware or wrapper to redact/strip secret values.
- [ ] Add and run CI tests simulating error/panic/crash, inspecting for leaked secrets.
- [ ] Document anti-patterns and logging best practices for reviewers.

**Test Strategy:** Static scanning, CI simulation, and log inspection. Manual and automated checks.

**Owner:** Core developers, CI lead

---

## 2. Enforce Strong REST API Authentication
**Objective:** API is always protected by a cryptographically strong bearer token. Startup fails if missing/weak.

**Milestones:**
- [ ] Define/document token strength requirements.
- [ ] Add startup validation for token presence/strength (fail if absent/weak/default).
- [ ] Update config samples (no defaults allowed).
- [ ] Add CI test for missing/weak token.

**Test Strategy:** Unit/integration tests for API startup paths. Manual review for config.

**Owner:** API dev, release/config engineer

---

## 3. Harden CORS Policy
**Objective:** REST API accepts cross-origin requests only from explicit, reviewed origins in production.

**Milestones:**
- [ ] Refactor CORS handling to require allowlist (prohibit `*` except in documented dev/test mode).
- [ ] Codify and document development-only override.
- [ ] CI/start-time check for prod origin list.
- [ ] Document config options/runbook for deployment.

**Test Strategy:** Unit/integration for origin policy, runtime verification, dev/prod diff.

**Owner:** API/infra, docs

---

## 4. Address Dependency Vulnerabilities
**Objective:** All dependencies current and verified free of known CVEs.

**Milestones:**
- [ ] Integrate CI pipeline for vulnerability scanning (e.g., govulncheck, Snyk, OS tooling).
- [ ] Triage and upgrade at-risk dependencies; re-test after upgrades.
- [ ] Automated report and CI blocking on new CVEs.
- [ ] Manual review as backup for non-automated paths.

**Test Strategy:** Automated scanner on merge. Human review monthly or per release.

**Owner:** Devops/tooling

---

## 5. Ensure Container Identity and File Permissions
**Objective:** Container always runs as non-root; config files/dirs limited to 644/755 permissions.

**Milestones:**
- [ ] Update Containerfile to `USER 1001:1001`. Refuse to start as root.
- [ ] Entrypoint verifies permissions for all config/files/directories.
- [ ] Add static and CI test for permissions regression.
- [ ] Document implications for deployments, file mounts, and K8s/infra.

**Test Strategy:** Container runtime, build, and static script tests; manual `docker/podman inspect` checks.

**Owner:** Devops

---

## 6. Secrets Hygiene and Rotation
**Objective:** No credentials in code or config samples; operators have clear rotation instructions.

**Milestones:**
- [ ] Static scan and manual repo audit for hardcoded secrets; replace with placeholders.
- [ ] Ban/prevent accidental check-in of secrets via tooling (pre-commit, git-secrets, Trufflehog).
- [ ] Add secret rotation docs/checklist and sample automation where possible.
- [ ] Document incident escalation in case of exposures.

**Test Strategy:** Static scan in CI/pipeline. Doc review for operators.

**Owner:** Security/domain owners, maintainers

---

## 7. Confirm Enforced REST API Rate Limiting
**Objective:** API cannot be overloaded/bruted; all violations logged and actionable.

**Milestones:**
- [ ] Integrate Go rate-limiting middleware for endpoints (10/sec, 20 burst default).
- [ ] Require config for rates—cannot disable in prod.
- [ ] Add CI/integration test for triggered limits (simulate DoS/brute-force).
- [ ] Wire logs to monitoring/alerting.

**Test Strategy:** Stress/integration tests; monitoring review.

**Owner:** API, monitoring/ops

---

## 8. Validate All Critical Environment Variables
**Objective:** Project never runs in partially configured/insecure state due to bad/missing env vars.

**Milestones:**
- [ ] Inventory/document all required environment variables (tokens, IDs, etc). 
- [ ] Implement centralized validation (startup fail fast with clear error if missing/malformed).
- [ ] Fuzz or negative tests for malformed/incomplete state.
- [ ] Update getting started/deployment/operator docs with validation instructions.

**Test Strategy:** Unit/integration for all error paths; manual doc review.

**Owner:** Core devs, ops

---

## Assumptions & Risks
- All environments/configs permit needed security changes (esp. non-root).
- Existing test, CI, and container infrastructure can be updated.
- No legacy usage depends on weaker/unsafe defaults removed by this plan.
- Dependencies' security tools provide current, reliable CVE results.

---

## Review, Testing, and Documentation Gates
- Each milestone must include CI/unit/integration coverage where technically possible, with manual test/audit as backup.
- All changes gated by code and doc review referencing relevant P1 item, rationale, and "why." 
- Documentation and operational runbooks updated in line with every owner milestone.
- No shipment allowed with incomplete milestones or failed review tests.

---

# END OF PLAN
