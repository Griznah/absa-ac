# Implementation Plan: Config File Upload/Download via Admin GUI

**Plan ID:** `ebdb4b53-946a-47b1-9d41-fef708d30bd7`
**Created:** 2026-03-02

## Overview

**Problem:** Admin GUI users cannot download the current config as a file or upload a new config file. Currently, users must manually copy/paste JSON through the browser editor.

**Approach:** Add dedicated download and upload endpoints to the API, with corresponding UI buttons in the admin frontend. Download returns config as a file attachment; upload accepts a JSON file and validates before applying.

## Constraints

| ID | Constraint |
|----|------------|
| C-001 | MUST follow existing vanilla JS SPA pattern (no framework) |
| C-002 | MUST use existing APIClient module for API calls |
| C-003 | MUST include CSRF token for state-changing requests |
| C-004 | MUST use existing middleware chain (auth, rate limit, CSRF) |
| C-005 | SHOULD follow existing file size limit of 1MB |

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

## Risks

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

---

## Milestones

### Wave 1: Backend Endpoints

#### M-001: Backend - Add config download endpoint

**Files:** `api/handlers.go`, `api/routes.go`

**Requirements:**
- GET /api/config/download returns config.json as downloadable file attachment
- Response includes Content-Disposition header with filename (config.json)
- Endpoint requires Bearer auth via existing middleware

**Acceptance Criteria:**
- curl with Bearer token downloads valid JSON config file
- Content-Disposition header present with attachment and filename
- Response status 200 with valid JSON body

**Code Changes:**

```go
// api/handlers.go - Add after ValidateConfig function

// DownloadConfig returns the configuration as a downloadable JSON file
// Requires Bearer token authentication
func (s *Server) DownloadConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.Context().Err(); err != nil {
		log.Printf("DownloadConfig cancelled: %v", err)
		WriteError(w, http.StatusServiceUnavailable, "Service unavailable", "Request cancelled")
		return
	}

	cfg := s.cm.GetConfigAny()

	// Set headers for file download
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=\"config.json\"")

	// Encode config as JSON
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		log.Printf("DownloadConfig encode error: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to encode config", err.Error())
		return
	}
}
```

```go
// api/routes.go - Add to RegisterRoutes function

mux.HandleFunc("GET /api/config/download", s.DownloadConfig)
```

---

#### M-002: Backend - Add config upload endpoint

**Files:** `api/handlers.go`, `api/routes.go`

**Requirements:**
- POST /api/config/upload accepts multipart/form-data with config file field
- Upload validates JSON syntax before applying
- 1MB max file size enforced
- Endpoint requires Bearer auth and CSRF token via existing middleware

**Acceptance Criteria:**
- Valid JSON file upload returns 200 with updated config
- Invalid JSON returns 400 with error details
- File over 1MB returns 413 Payload Too Large
- Missing file field returns 400 Bad Request

**Code Changes:**

```go
// api/handlers.go - Add after DownloadConfig function

// UploadConfig accepts a config file upload and applies it
// Requires Bearer token authentication and CSRF token
func (s *Server) UploadConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.Context().Err(); err != nil {
		log.Printf("UploadConfig cancelled: %v", err)
		WriteError(w, http.StatusServiceUnavailable, "Service unavailable", "Request cancelled")
		return
	}

	// Limit upload size to 1MB
	const maxUploadSize = 1 << 20 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	// Parse multipart form
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		if err.Error() == "http: request body too large" {
			WriteError(w, http.StatusRequestEntityTooLarge, "File too large", "Maximum size is 1MB")
			return
		}
		WriteError(w, http.StatusBadRequest, "Failed to parse form", err.Error())
		return
	}

	// Get file from form field "config"
	file, header, err := r.FormFile("config")
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Missing file", "No file found in 'config' field")
		return
	}
	defer file.Close()

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".json") {
		WriteError(w, http.StatusBadRequest, "Invalid file type", "Only .json files are accepted")
		return
	}

	// Read file content
	data, err := io.ReadAll(file)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to read file", err.Error())
		return
	}

	// Validate JSON syntax
	var newConfig map[string]interface{}
	if err := json.Unmarshal(data, &newConfig); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}

	// Write config (triggers backup rotation via WriteConfigAny)
	if err := s.cm.WriteConfigAny(newConfig); err != nil {
		WriteError(w, http.StatusBadRequest, "Config write failed", err.Error())
		return
	}

	// Return updated config
	cfg := s.cm.GetConfigAny()
	WriteJSON(w, http.StatusOK, cfg)
}
```

```go
// api/routes.go - Add to RegisterRoutes function

mux.HandleFunc("POST /api/config/upload", s.UploadConfig)
```

**Required imports for handlers.go:**
```go
// Add to existing imports:
import (
	"encoding/json"
	"io"          // ADD THIS
	"log"
	"net/http"
	"strings"     // ADD THIS
)
```

---

### Wave 2: Frontend

#### M-003: Frontend - Add download/upload buttons and handlers

**Files:** `api/web/admin/index.html`, `api/web/admin/app.js`, `api/web/admin/api.js`

**Requirements:**
- Download and Upload buttons added to actions section
- Download triggers browser save-as dialog with config file
- Upload opens file picker filtered to .json files
- Upload shows success/error message after completion
- Follow existing vanilla JS SPA pattern (no framework)

**Acceptance Criteria:**
- Clicking Download button downloads config file
- Clicking Upload button opens file picker
- Selecting valid JSON file uploads and shows success message
- Selecting invalid file shows error message

**Code Changes:**

```html
<!-- api/web/admin/index.html - Modify actions section -->
<!-- Find the existing actions section (around line 72-75) and replace with: -->

<section class="config-section actions">
    <button id="validate-btn">Validate</button>
    <button id="save-btn">Save Changes</button>
    <button id="download-btn">Download Config</button>
    <button id="upload-btn">Upload Config</button>
    <input type="file" id="file-input" accept=".json" class="hidden">
</section>

<!-- Note: .hidden class already exists in styles.css at line 46-48 -->
```

```javascript
// api/web/admin/api.js - Add new methods to APIClient object

// Download config as file
async downloadConfig() {
    const headers = this.buildHeaders(false);
    const response = await fetch(`${this.baseURL}/config/download`, { headers });

    if (!response.ok) {
        return { ok: false, status: response.status, error: await this.parseError(response) };
    }

    // Get filename from Content-Disposition header
    const disposition = response.headers.get('Content-Disposition');
    let filename = 'config.json';
    if (disposition) {
        const match = disposition.match(/filename="(.+)"/);
        if (match) filename = match[1];
    }

    // Trigger browser download
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);

    return { ok: true, status: response.status };
},

// Upload config file
async uploadConfig(file) {
    const formData = new FormData();
    formData.append('config', file);

    const headers = {};
    const bearerToken = window.Auth?.getToken();
    if (bearerToken && bearerToken !== 'proxy') {
        headers['Authorization'] = `Bearer ${bearerToken}`;
    }
    const csrfToken = window.Auth?.getCSRFToken();
    if (csrfToken) {
        headers['X-CSRF-Token'] = csrfToken;
    }

    const response = await fetch(`${this.baseURL}/config/upload`, {
        method: 'POST',
        headers,
        body: formData
    });

    if (response.status === 401) {
        window.Auth?.logout();
        return { ok: false, status: 401, error: 'Authentication required' };
    }

    if (response.ok) {
        const data = await response.json();
        return { ok: true, status: response.status, data };
    }

    return { ok: false, status: response.status, error: await this.parseError(response) };
}
```

```javascript
// api/web/admin/app.js - Add to bindEvents function

// Download button
document.getElementById('download-btn').addEventListener('click', () => {
    this.handleDownload();
});

// Upload button
document.getElementById('upload-btn').addEventListener('click', () => {
    document.getElementById('file-input').click();
});

// File input change
document.getElementById('file-input').addEventListener('change', (e) => {
    this.handleFileSelect(e);
});
```

```javascript
// api/web/admin/app.js - Add new handler methods

// Handle download button click
async handleDownload() {
    const response = await window.APIClient.downloadConfig();
    if (response.ok) {
        this.showMessage('Config downloaded', 'success');
    } else {
        this.showMessage('Download failed: ' + response.error, 'error');
    }
},

// Handle file selection for upload
async handleFileSelect(e) {
    const file = e.target.files[0];
    if (!file) return;

    // Validate file extension
    if (!file.name.endsWith('.json')) {
        this.showMessage('Please select a .json file', 'error');
        e.target.value = ''; // Reset file input
        return;
    }

    const response = await window.APIClient.uploadConfig(file);
    if (response.ok) {
        this.showMessage('Config uploaded successfully', 'success');
        await this.loadConfig(); // Refresh from server
    } else {
        this.showMessage('Upload failed: ' + response.error, 'error');
    }

    e.target.value = ''; // Reset file input
}
```

**Note:** The `.hidden` CSS class already exists in styles.css (line 46-48), no CSS changes needed.

---

### Wave 3: Tests

#### M-004: Tests - Add handler and integration tests for download/upload

**Files:** `api/handlers_test.go`, `api/e2e_test.go`

**Requirements:**
- Unit tests for DownloadConfig and UploadConfig handlers
- E2E test for download-upload roundtrip
- Tests cover success and error scenarios

**Acceptance Criteria:**
- `go test -v -run TestDownloadConfig` passes
- `go test -v -run TestUploadConfig` passes
- `go test -v -run TestE2E` passes with download/upload scenarios

**Code Changes:**

```go
// api/handlers_test.go - Add tests
// NOTE: Uses existing mockConfigManagerWithWrites from handlers_test.go

func TestDownloadConfig(t *testing.T) {
	cm := &mockConfigManagerWithWrites{config: map[string]interface{}{"servers": []interface{}{}}}
	s := NewServer(cm, "3001", "test-token", nil, nil, log.New(os.Stdout, "TEST: ", log.LstdFlags))

	req := httptest.NewRequest("GET", "/api/config/download", nil)
	rec := httptest.NewRecorder()

	s.DownloadConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Error("expected Content-Disposition attachment header")
	}

	if !strings.Contains(rec.Header().Get("Content-Disposition"), "config.json") {
		t.Error("expected filename in Content-Disposition header")
	}
}

func TestUploadConfig(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		cm := &mockConfigManagerWithWrites{config: map[string]interface{}{}}
		s := NewServer(cm, "3001", "test-token", nil, nil, log.New(os.Stdout, "TEST: ", log.LstdFlags))

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("config", "test.json")
		part.Write([]byte(`{"servers": []}`))
		writer.Close()

		req := httptest.NewRequest("POST", "/api/config/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		s.UploadConfig(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		cm := &mockConfigManagerWithWrites{config: map[string]interface{}{}}
		s := NewServer(cm, "3001", "test-token", nil, nil, log.New(os.Stdout, "TEST: ", log.LstdFlags))

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("config", "test.json")
		part.Write([]byte(`{invalid json}`))
		writer.Close()

		req := httptest.NewRequest("POST", "/api/config/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		s.UploadConfig(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("missing file field", func(t *testing.T) {
		cm := &mockConfigManagerWithWrites{config: map[string]interface{}{}}
		s := NewServer(cm, "3001", "test-token", nil, nil, log.New(os.Stdout, "TEST: ", log.LstdFlags))

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		writer.Close()

		req := httptest.NewRequest("POST", "/api/config/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		s.UploadConfig(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("non-JSON file extension", func(t *testing.T) {
		cm := &mockConfigManagerWithWrites{config: map[string]interface{}{}}
		s := NewServer(cm, "3001", "test-token", nil, nil, log.New(os.Stdout, "TEST: ", log.LstdFlags))

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("config", "test.txt")
		part.Write([]byte(`{"servers": []}`))
		writer.Close()

		req := httptest.NewRequest("POST", "/api/config/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		s.UploadConfig(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})
}
```

**Required imports for handlers_test.go:**
```go
// Add to existing imports:
import (
	"bytes"       // ADD THIS
	"encoding/json"
	"log"
	"mime/multipart"  // ADD THIS
	"net/http"
	"net/http/httptest"
	"os"          // ADD THIS (already exists)
	"strings"
	"testing"
)
```

```go
// api/e2e_test.go - Add E2E test
// NOTE: Uses existing setupTestEnvironment() from e2e_test.go

func TestE2EConfigDownloadUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	// Use existing test infrastructure
	initialConfig := generateConfig(2)
	client, baseURL, cleanup := setupTestEnvironment(t, initialConfig)
	defer cleanup()

	t.Run("download-upload roundtrip", func(t *testing.T) {
		// Download config
		downloadResp, err := client.Get(baseURL + "/api/config/download")
		if err != nil {
			t.Fatalf("download failed: %v", err)
		}
		defer downloadResp.Body.Close()

		if downloadResp.StatusCode != http.StatusOK {
			t.Fatalf("download returned %d, expected 200", downloadResp.StatusCode)
		}

		downloadedData, _ := io.ReadAll(downloadResp.Body)

		// Verify Content-Disposition header
		disposition := downloadResp.Header.Get("Content-Disposition")
		if !strings.Contains(disposition, "attachment") {
			t.Error("expected Content-Disposition attachment header")
		}

		// Upload same config
		uploadBody := &bytes.Buffer{}
		writer := multipart.NewWriter(uploadBody)
		part, _ := writer.CreateFormFile("config", "config.json")
		part.Write(downloadedData)
		writer.Close()

		uploadReq, _ := http.NewRequest("POST", baseURL+"/api/config/upload", uploadBody)
		uploadReq.Header.Set("Content-Type", writer.FormDataContentType())

		uploadResp, err := client.Do(uploadReq)
		if err != nil {
			t.Fatalf("upload failed: %v", err)
		}
		defer uploadResp.Body.Close()

		if uploadResp.StatusCode != http.StatusOK {
			t.Errorf("upload returned %d, expected 200", uploadResp.StatusCode)
		}
	})
}
```

---

## Implementation Order

1. **Wave 1: Backend** (M-001, M-002)
   - Add DownloadConfig handler and route
   - Add UploadConfig handler and route
   - Test with curl

2. **Wave 2: Frontend** (M-003)
   - Add buttons to index.html
   - Add API methods to api.js
   - Add handlers to app.js
   - Test in browser

3. **Wave 3: Tests** (M-004)
   - Add unit tests for handlers
   - Add E2E tests
   - Run full test suite

## Testing Commands

```bash
# Test download endpoint
curl -H "Authorization: Bearer $TOKEN" http://localhost:3001/api/config/download -o config.json

# Test upload endpoint
curl -X POST -H "Authorization: Bearer $TOKEN" -F "config=@config.json" http://localhost:3001/api/config/upload

# Run unit tests
go test -v -run TestDownloadConfig ./api/
go test -v -run TestUploadConfig ./api/

# Run all tests
go test -v ./api/...
```
