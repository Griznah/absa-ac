# Security Guide & Credential Rotation

## Security Contact

- Primary: [maintainer@example.com](mailto:maintainer@example.com)
- For urgent security notifications, add an ADMIN or published email here

## Credential Rotation & Incident Runbook

### When to Rotate Secrets
- On suspected or confirmed compromise/leak (token or config committed or observed in logs)
- When an operator leaves or changes role
- At each scheduled quarterly review
- Immediately after notifying Discord or API partner of a token complaint/incident

### How to Rotate API_BEARER_TOKEN
1. Generate a new random value:
   ```bash
   head -c 48 /dev/urandom | base64
   ```
2. Set `API_BEARER_TOKEN` in your `.env` file or environment, or secret store
3. Redeploy/update your containers and/or CI variables
4. Confirm bot and API servers accept only the new token; old one is no longer valid
5. Remove/rotate any cached or backup configs storing the old token

### How to Rotate Discord Bot Token
1. Go to the [Discord Developer Portal](https://discord.com/developers/applications)
2. Regenerate the token in your application settings
3. Update `.env`, CI/CD secrets, or orchestration variables with the new token
4. Redeploy bot/container; verify login is successful
5. Revoke any old credentials, and verify no config/history retains old ones

### If a Secret Is Leaked/Committed
1. **Immediate Steps:**
    - Remove the secret from the repo history (git rm, filter-branch, or BFG)
    - Rotate the leaked secret(s) as above
    - Inform any affected parties
2. **Hardening:**
    - Check logs for evidence of misuse
    - Schedule additional credential rotation if needed
    - Audit CI logs, PR comments, GitHub Actions artifacts
3. **Prevention:**
    - Confirm `truffleHog` and push protections are enabled and unbroken in CI
    - Rebrief team on credential hygiene

---

# Pre-release and Operational Security Checklist

- [ ] All changes have been reviewed for absence of tokens/secrets outside .env.example and config.json.example
- [ ] CI passes all static secret scans (truffleHog or equivalent)
- [ ] API_BEARER_TOKEN is strong, not a default/placeholder, and rotated if ever compromised
- [ ] Discord Bot Token was securely rotated and all prior tokens revoked after any incident
- [ ] CI containers build as USER 1001 (never root); image-test job is enforced
- [ ] All dependencies have been scanned for vulnerabilities (govulncheck/CI job)
- [ ] CORS, API tokens, and ENV are documented, reviewed, and no wildcards are present in production
- [ ] Production config files are kept out of version control (gitignore)
- [ ] Team is aware of incident response process as outlined above
- [ ] Operational staff know how to rotate tokens, re-deploy, and trigger secret scans
- [ ] Last security review <90d ago; schedule next review

## Usage
- Use this checklist before each release or new deployment
- Incorporate into internal runbooks or hand-off docs
- Update this file as new risks or operational steps are discovered
