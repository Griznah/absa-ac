# Config File Upload/Download via Admin GUI — Decision Record

Admin GUI users could not download the current config as a file or upload a new one — only copy/paste JSON through the browser editor. Added dedicated download/upload endpoints and UI buttons.

Status: implemented. Code and git history are the implementation record; this file preserves design rationale.

## Decisions

| ID | Decision | Reasoning |
|----|----------|-----------|
| DL-001 | Add dedicated file download/upload endpoints (`GET /api/config/download`, `POST /api/config/upload`) instead of reusing existing GET/PUT endpoints | Existing GET /api/config returns JSON for in-browser editing; File download needs Content-Disposition header; Upload needs multipart/form-data parsing; Separate endpoints provide clear separation of concerns |
| DL-002 | Use existing 1MB body size limit for uploads | Config files typically small (< 100KB); Existing 1MB limit prevents memory exhaustion; Consistent with PUT /api/config behavior |
| DL-003 | Upload validates JSON syntax before writing config | Invalid JSON would corrupt config file; ConfigManager.WriteConfigAny expects valid map; Fail-fast prevents broken state |
| DL-004 | Download returns config.json raw content with Content-Disposition attachment header | Browser triggers save-as dialog; Filename includes timestamp for version tracking; Content-Type: application/json for proper handling |
| DL-005 | Frontend uses hidden file input with button trigger for upload UX | File input styling limited across browsers; Button provides consistent UI with existing actions; Standard pattern for file upload forms |
| DL-006 | 1MB limit enforced at handler level using `r.ParseMultipartForm(1<<20)` before reading file | Existing pattern in handlers uses body size limits; Multipart form needs explicit size limit; Handler-level check provides clear error messages |
| DL-007 | Upload does NOT block config reload; in-flight uploads complete with config version they started with | ConfigManager.GetConfigAny returns snapshot; Upload reads file into memory before calling WriteConfigAny; Race condition unlikely |
| DL-008 | Multipart form field name for config file is `config` (lowercase) | Matches existing API convention; Simple, descriptive name |
| DL-009 | Upload error responses use standard JSON format: `{"error": "error_type", "message": "details"}` with appropriate HTTP status codes | Matches existing error response format; Consistent with GET/PUT /api/config error handling |
| DL-010 | .json file filtering enforced on both frontend (accept attribute) and backend (file extension validation before parsing) | Frontend accept=.json provides UX hint; Backend validation prevents bypass via direct API calls; Defense in depth |
| DL-011 | Upload DOES trigger ConfigManager backup rotation (same as PUT /api/config) | WriteConfigAny internally handles backup rotation; Consistent behavior between PUT and upload |

## Risks (mitigated)

| ID | Risk | Mitigation |
|----|------|------------|
| R-001 | Malformed JSON upload could corrupt config if validation fails | DL-003 requires JSON validation before WriteConfigAny; ConfigManager backup rotation (DL-011) provides rollback |
| R-002 | Large file upload could cause memory exhaustion | DL-006 enforces 1MB limit at handler level via ParseMultipartForm |
| R-003 | Concurrent upload and config reload could cause race condition | DL-007 accepts this risk; uploads and reloads are rare; ConfigManager handles atomic writes |

## Rejected Alternatives

| ID | Alternative | Rejection Reason |
|----|-------------|------------------|
| RA-001 | Reuse GET /api/config with Accept header for download | Requires client to set Accept header and handle blob differently; simpler to have dedicated endpoint with Content-Disposition always set |
| RA-002 | Reuse PUT /api/config for upload with base64-encoded file | Base64 encoding increases payload size by 33%; multipart/form-data is standard for file uploads |
