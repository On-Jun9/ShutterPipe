package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/On-Jun9/ShutterPipe/internal/pipeline"
	"github.com/On-Jun9/ShutterPipe/internal/state"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type multipartTestFile struct {
	field   string
	name    string
	content string
}

type manifestTempStub struct {
	name     string
	writeErr error
	closeErr error
}

func (f *manifestTempStub) Write(data []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(data), nil
}

func (f *manifestTempStub) Close() error { return f.closeErr }

func (f *manifestTempStub) Name() string { return f.name }

func buildVerifyMultipart(t *testing.T, configJSON *string, files []multipartTestFile) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if configJSON != nil {
		if err := writer.WriteField("config", *configJSON); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range files {
		part, err := writer.CreateFormFile(file.field, file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(file.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}

func TestHandleVerifyStartsSharedRunAndRetainsTerminalSummary(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	sourceDir := filepath.Join(root, "source")
	destDir := filepath.Join(root, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "photo.jpg"), []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "PHOTO_1.JPG"), []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	configJSON := `{
		"run_id":"verify-run",
		"verify_mode":"quick",
		"source":` + quoteJSON(t, sourceDir) + `,
		"dest":` + quoteJSON(t, destDir) + `,
		"include_extensions":["jpg"],
		"jobs":1
	}`
	body, contentType := buildVerifyMultipart(t, &configJSON, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/verify", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s := &Server{hub: NewHub()}

	s.handleVerify(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var started StartRunResponse
	if err := json.NewDecoder(rr.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	if started.Kind != types.RunKindVerify || started.RunID != "verify-run" {
		t.Fatalf("unexpected start response: %+v", started)
	}
	waitForRunManagerInactive(t, s)

	status := s.runs().StatusFor("verify-run")
	if status.Status != RunStatusComplete || status.Kind != types.RunKindVerify || status.VerifySummary == nil {
		t.Fatalf("unexpected terminal status: %+v", status)
	}
	if status.VerifySummary.Normal != 1 || status.VerifySummary.ProblemCount != 0 {
		t.Fatalf("unexpected verify summary: %+v", status.VerifySummary)
	}
	if _, err := os.Stat(filepath.Join(home, ".shutterpipe", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("verify-only run created state: %v", err)
	}
}

func TestHandleVerifyRejectsManifestOutsideHashModeAndCleansUpload(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	tempDir := filepath.Join(root, "tmp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tempDir)
	sourceDir := filepath.Join(root, "source")
	destDir := filepath.Join(root, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	configJSON := `{"verify_mode":"quick","source":` + quoteJSON(t, sourceDir) + `,"dest":` + quoteJSON(t, destDir) + `}`
	body, contentType := buildVerifyMultipart(t, &configJSON, []multipartTestFile{{
		field: "hash_manifest", name: "hashes.txt", content: "not-empty",
	}})
	req := httptest.NewRequest(http.MethodPost, "/api/verify", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()

	(&Server{}).handleVerify(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	matches, err := filepath.Glob(filepath.Join(tempDir, "shutterpipe-verify-manifest-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary uploads remain: %v", matches)
	}
}

func TestHandleVerifyRejectsDuplicateAndEmptyManifest(t *testing.T) {
	configJSON := `{"verify_mode":"hash","source":"/source","dest":"/dest"}`
	tests := []struct {
		name  string
		files []multipartTestFile
	}{
		{
			name: "duplicate",
			files: []multipartTestFile{
				{field: "hash_manifest", name: "one.txt", content: "one"},
				{field: "hash_manifest", name: "two.txt", content: "two"},
			},
		},
		{
			name:  "empty",
			files: []multipartTestFile{{field: "hash_manifest", name: "empty.txt"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, contentType := buildVerifyMultipart(t, &configJSON, test.files)
			req := httptest.NewRequest(http.MethodPost, "/api/verify", body)
			req.Header.Set("Content-Type", contentType)
			rr := httptest.NewRecorder()
			(&Server{}).handleVerify(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestParseVerifyUploadTreatsManifestTempFailuresAsInternalErrors(t *testing.T) {
	configJSON := `{"verify_mode":"hash","source":"/source","dest":"/dest"}`
	tests := []struct {
		name     string
		writeErr error
		closeErr error
	}{
		{name: "write failure", writeErr: errors.New("disk full")},
		{name: "close failure", closeErr: errors.New("flush failed")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, contentType := buildVerifyMultipart(t, &configJSON, []multipartTestFile{{
				field: "hash_manifest", name: "hashes.txt", content: "not-empty",
			}})
			req := httptest.NewRequest(http.MethodPost, "/api/verify", body)
			req.Header.Set("Content-Type", contentType)
			rr := httptest.NewRecorder()
			temp := &manifestTempStub{
				name: filepath.Join(t.TempDir(), "manifest.tmp"), writeErr: test.writeErr, closeErr: test.closeErr,
			}

			_, status, err := parseVerifyUploadWithTemp(rr, req, func() (verifyManifestTemp, error) {
				return temp, nil
			})
			if err == nil {
				t.Fatal("expected temporary manifest failure")
			}
			if status != http.StatusInternalServerError {
				t.Fatalf("status=%d, want %d", status, http.StatusInternalServerError)
			}
		})
	}
}

func TestHandleVerifyRequeueAppliesAndThenSkipsIdempotently(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	sourcePath := filepath.Join(root, "source.jpg")
	if err := os.WriteFile(sourcePath, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := types.FileEntry{
		Path: sourcePath, Name: "source.jpg", Size: info.Size(), ModTime: info.ModTime(),
	}
	stateFile := filepath.Join(root, "state", "state.json")
	s := &Server{}
	serverID := s.instanceID()
	s.storeLatestVerification(&retainedVerification{
		RunID: "verify-run", ServerID: serverID, StateFile: stateFile,
		Summary: &types.VerifySummary{RequeueAllowed: true, RequeueEligible: 1},
		Candidates: []pipeline.RequeueCandidate{{
			Entry: entry,
			SourceContext: state.SourceContext{
				ConfigFingerprint: "fingerprint",
			},
			Verdict: types.VerifyVerdictMissing,
			Mode:    types.VerifyModeQuick,
		}},
	})

	call := func() verifyRequeueResponse {
		t.Helper()
		requestBody, err := json.Marshal(verifyRequeueRequest{RunID: "verify-run", ServerID: serverID})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/verify/requeue", bytes.NewReader(requestBody))
		rr := httptest.NewRecorder()
		s.handleVerifyRequeue(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
		var response verifyRequeueResponse
		if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		return response
	}

	first := call()
	if first.Applied != 1 || first.Skipped != 0 {
		t.Fatalf("first response: %+v", first)
	}
	second := call()
	if second.Applied != 0 || second.Skipped != 1 {
		t.Fatalf("second response: %+v", second)
	}
	loaded, err := state.Load(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.RebackupMarker(sourcePath); !ok {
		t.Fatal("requeue marker was not persisted")
	}
}

func TestHandleVerifyRequeueRejectsWhileRunIsActive(t *testing.T) {
	s := &Server{}
	serverID := s.instanceID()
	if !s.runs().Start("active-run", func() {}) {
		t.Fatal("failed to start test run")
	}
	body, err := json.Marshal(verifyRequeueRequest{RunID: "verify-run", ServerID: serverID})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/verify/requeue", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleVerifyRequeue(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	s.runs().Finish("active-run", RunStatusComplete, nil, "")
}

func TestHandleVerifyRequeueRejectsTrailingBody(t *testing.T) {
	s := &Server{}
	body, err := json.Marshal(verifyRequeueRequest{RunID: "verify-run", ServerID: s.instanceID()})
	if err != nil {
		t.Fatal(err)
	}
	payload := append(body, []byte(`{"run_id":"second"}`)...)
	req := httptest.NewRequest(http.MethodPost, "/api/verify/requeue", bytes.NewReader(payload))
	rr := httptest.NewRecorder()
	s.handleVerifyRequeue(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func quoteJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
