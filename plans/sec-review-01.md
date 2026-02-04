# Security P1 Issues – Planning Checklist

Each point listed below is a high-priority security risk that must be addressed in upcoming development cycles for robust project hygiene and risk mitigation.

---

### 1. Prevent Secrets Exposure  
**Why:** Leaked Discord/API tokens allow account takeover or control of infrastructure.  
**Action:** Audit all error logging, debugging, and trace output. Ensure secrets are never printed, leaked, or stored in logs.  
**Outcome:** No code path ever exposes Discord or API tokens outside safe process memory.

---

### 2. Enforce Strong REST API Authentication  
**Why:** Unset or weak API_BEARER_TOKEN allows unauthorized config or bot control.  
**Action:** Require API_BEARER_TOKEN to be set and sufficiently random on any API-enabled run. API startup must fail if missing or weak.  
**Outcome:** REST API is always protected by a secure bearer token; cannot run with empty/default/weak token.

---

### 3. Harden CORS Policy  
**Why:** Wildcard CORS policies (`*`) permit cross-origin forgery and data theft.  
**Action:** Disallow `*` as allowed origin except for dedicated dev/test builds. Require documented, explicit allowlist for API_CORS_ORIGINS in production.  
**Outcome:** REST API accepts cross-origin requests only from intended, reviewed origins.

---

### 4. Address Dependency Vulnerabilities  
**Why:** Unpatched third-party vulnerabilities may allow remote exploits or privilege escalation.  
**Action:** Audit dependency CVEs for all libraries (discordgo, websocket, crypto, etc.) and upgrade any at risk.  
**Outcome:** All dependencies are current and verified free of known security issues.

---

### 5. Ensure Container Identity and File Permissions  
**Why:** Root containers or lax file permissions enable escape, privilege escalation, or config tampering.  
**Action:** Check container always runs as non-root (UID/GID 1001). Volume-mounted config files/dirs limited to 644/755 permissions, with no broader access.  
**Outcome:** Container processes and critical files are always least-privilege and secure from tampering.

---

### 6. Secrets Hygiene and Rotation  
**Why:** Hardcoded secrets and stale tokens are frequent breach and lateral movement vectors.  
**Action:** Audit repo and examples for hardcoded secrets. Document, automate, and review secret rotation practices.  
**Outcome:** No credentials are present in code or config samples; operators have clear rotation instructions.

---

### 7. Confirm Enforced REST API Rate Limiting  
**Why:** Uncontrolled API request volume enables DoS or brute-force attacks.  
**Action:** Validate proper rate limiting (10/sec, 20 burst) and log all limit violations. Monitor for unexpected request surges.  
**Outcome:** API cannot be overloaded or brute-forced; excessive usage is always recorded and actionable.

---

### 8. Validate All Critical Environment Variables  
**Why:** Missing or malformed env vars (tokens, channel IDs) can break runtime security or cause undefined bot behavior.  
**Action:** Implement startup validation for required vars—empty, malformed, or out-of-range values must halt the bot with a clear error.  
**Outcome:** Project never runs in a partially configured or insecure state due to environment issues.

---

**Next Steps for Planning:**

- Prioritize these P1 issues for review and remediation in the next sprint or cycle.
- Assign specific engineers/owners for each area and track status visibly.
- Incorporate these actions into CI tests, operational docs, and code review checklists.
