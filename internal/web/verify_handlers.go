package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/internal/pipeline"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

const (
	maxHashManifestBytes  = int64(64 * 1024 * 1024)
	maxVerifyConfigBytes  = int64(1024 * 1024)
	maxVerifyRequestBytes = maxHashManifestBytes + maxVerifyConfigBytes + 2*1024*1024
)

type verifyRunRequest struct {
	config.Config
	RunID      string           `json:"run_id,omitempty"`
	VerifyMode types.VerifyMode `json:"verify_mode"`
}

type verifyUpload struct {
	configData   []byte
	manifestPath string
	manifestName string
}

type verifyManifestTemp interface {
	io.WriteCloser
	Name() string
}

type recordingWriter struct {
	writer io.Writer
	err    error
}

func (w *recordingWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	if err != nil {
		w.err = err
	}
	return written, err
}

type retainedVerification struct {
	RunID      string
	ServerID   string
	StateFile  string
	Summary    *types.VerifySummary
	Candidates []pipeline.RequeueCandidate
}

type verifyRequeueRequest struct {
	RunID    string `json:"run_id"`
	ServerID string `json:"server_id"`
}

type verifyRequeueResponse struct {
	Applied int `json:"applied"`
	Skipped int `json:"skipped"`
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	if s.isShuttingDown() {
		writeAPIError(w, http.StatusServiceUnavailable, "server is shutting down")
		return
	}
	if s.runs().IsActive() {
		writeAPIError(w, http.StatusConflict, "another operation is already running")
		return
	}

	upload, status, err := parseVerifyUpload(w, r)
	if err != nil {
		writeAPIError(w, status, err.Error())
		return
	}
	uploadOwned := true
	defer func() {
		if uploadOwned && upload.manifestPath != "" {
			_ = os.Remove(upload.manifestPath)
		}
	}()

	var req verifyRunRequest
	if err := json.Unmarshal(upload.configData, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.VerifyMode != types.VerifyModeQuick && req.VerifyMode != types.VerifyModeHash {
		writeValidationError(w, "verify_mode", "must be one of quick, hash")
		return
	}
	if upload.manifestPath != "" && req.VerifyMode != types.VerifyModeHash {
		writeValidationError(w, "hash_manifest", "is only allowed in hash mode")
		return
	}
	if err := req.Config.Validate(); err != nil {
		var validationErr *config.ValidationError
		if errors.As(err, &validationErr) {
			writeValidationError(w, validationErr.Field, validationErr.Message)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if r.Context().Err() != nil {
		return
	}

	runID := req.RunID
	if runID == "" {
		runID, err = newRunID()
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if len(runID) > 128 {
		writeAPIError(w, http.StatusBadRequest, "run_id is too long")
		return
	}

	runCtx, cancelRun, startStatus, err := s.startRunKind(runID, types.RunKindVerify)
	if errors.Is(err, http.ErrServerClosed) {
		writeAPIError(w, http.StatusServiceUnavailable, "server is shutting down")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	s.clearLatestVerification()
	if r.Context().Err() != nil {
		cancelRun()
		s.finishRun(pipeline.ProgressUpdate{
			Type: "cancelled", Kind: types.RunKindVerify, RunID: runID,
			Message: "start request was cancelled before execution",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(StartRunResponse{
		Status: "started", Kind: types.RunKindVerify,
		RunID: runID, ServerID: s.instanceID(), Revision: startStatus.Revision,
	})

	uploadOwned = false
	go s.runVerification(runCtx, cancelRun, runID, &req, upload)
}

func (s *Server) runVerification(runCtx context.Context, cancelRun func(), runID string, req *verifyRunRequest, upload verifyUpload) {
	defer cancelRun()
	var terminalUpdate pipeline.ProgressUpdate
	terminalPending := false
	defer func() {
		if recovered := recover(); recovered != nil {
			fmt.Printf("PANIC RECOVERED: %v\n", recovered)
			terminalUpdate = pipeline.ProgressUpdate{
				Type: "error", Kind: types.RunKindVerify, RunID: runID,
				Error: fmt.Sprintf("Internal Server Error: %v", recovered),
			}
			terminalPending = true
		}
		if upload.manifestPath != "" {
			_ = os.Remove(upload.manifestPath)
		}
		if !terminalPending {
			terminalUpdate = pipeline.ProgressUpdate{
				Type: "error", Kind: types.RunKindVerify, RunID: runID,
				Error: "verification ended without a terminal result",
			}
		}
		s.finishRun(terminalUpdate)
	}()

	verification, err := pipeline.NewVerification(&req.Config, req.VerifyMode, upload.manifestPath, upload.manifestName)
	if err != nil {
		terminalUpdate = pipeline.ProgressUpdate{
			Type: "error", Kind: types.RunKindVerify, RunID: runID, Error: err.Error(),
		}
		terminalPending = true
		return
	}
	defer func() {
		if closeErr := verification.Close(); closeErr != nil {
			fmt.Printf("WARNING: failed to close verification logger for run %s: %v\n", runID, closeErr)
		}
	}()

	verification.SetProgressCallback(func(update pipeline.ProgressUpdate) {
		update.Kind = types.RunKindVerify
		update.RunID = runID
		if isTerminalProgressType(update.Type) {
			terminalUpdate = update
			terminalPending = true
			return
		}
		s.broadcastActiveRunProgress(update)
	})

	result, err := verification.RunWithContext(runCtx)
	if err != nil {
		var summary *types.VerifySummary
		if result != nil {
			summary = result.Summary
		}
		if errors.Is(err, pipeline.ErrRunCanceled) {
			terminalUpdate = pipeline.ProgressUpdate{
				Type: "cancelled", Kind: types.RunKindVerify, RunID: runID,
				Message: "검증이 취소되었습니다.", VerifySummary: summary,
			}
		} else {
			terminalUpdate = pipeline.ProgressUpdate{
				Type: "error", Kind: types.RunKindVerify, RunID: runID,
				VerifySummary: summary, Error: err.Error(),
			}
		}
		terminalPending = true
		return
	}

	s.storeLatestVerification(&retainedVerification{
		RunID: runID, ServerID: s.instanceID(), StateFile: req.StateFile,
		Summary: result.Summary, Candidates: result.RequeueCandidates,
	})
	if !terminalPending {
		terminalUpdate = pipeline.ProgressUpdate{
			Type: "complete", Kind: types.RunKindVerify, RunID: runID,
			VerifySummary: result.Summary,
		}
		terminalPending = true
	}
}

func (s *Server) handleVerifyRequeue(w http.ResponseWriter, r *http.Request) {
	var req verifyRequeueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.RunID == "" {
		writeAPIError(w, http.StatusBadRequest, "run_id is required")
		return
	}
	if req.ServerID == "" {
		writeAPIError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	if req.ServerID != s.instanceID() {
		writeAPIError(w, http.StatusConflict, "server_id does not match the current server")
		return
	}

	release, err := s.startMutation()
	if errors.Is(err, http.ErrServerClosed) {
		writeAPIError(w, http.StatusServiceUnavailable, "server is shutting down")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusConflict, "실행 중에는 처리할 수 없습니다.")
		return
	}
	defer release()

	retained := s.latestVerification(req.RunID, req.ServerID)
	if retained == nil || retained.Summary == nil || !retained.Summary.RequeueAllowed {
		writeAPIError(w, http.StatusConflict, "보관된 검증 결과가 없습니다. 다시 검증해 주세요.")
		return
	}
	result, err := pipeline.QueueVerificationProblems(r.Context(), retained.StateFile, req.RunID, retained.Candidates)
	if errors.Is(err, pipeline.ErrRunAlreadyActive) {
		writeAPIError(w, http.StatusConflict, "실행 중에는 처리할 수 없습니다.")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(verifyRequeueResponse{Applied: result.Applied, Skipped: result.Stale})
}

func (s *Server) clearLatestVerification() {
	s.verifyResultMu.Lock()
	s.latestVerify = nil
	s.verifyResultMu.Unlock()
}

func (s *Server) storeLatestVerification(result *retainedVerification) {
	cloned := *result
	cloned.Summary = cloneVerifySummary(result.Summary)
	cloned.Candidates = append([]pipeline.RequeueCandidate(nil), result.Candidates...)
	s.verifyResultMu.Lock()
	s.latestVerify = &cloned
	s.verifyResultMu.Unlock()
}

func (s *Server) latestVerification(runID, serverID string) *retainedVerification {
	s.verifyResultMu.Lock()
	defer s.verifyResultMu.Unlock()
	if s.latestVerify == nil || s.latestVerify.RunID != runID || s.latestVerify.ServerID != serverID {
		return nil
	}
	cloned := *s.latestVerify
	cloned.Summary = cloneVerifySummary(s.latestVerify.Summary)
	cloned.Candidates = append([]pipeline.RequeueCandidate(nil), s.latestVerify.Candidates...)
	return &cloned
}

func parseVerifyUpload(w http.ResponseWriter, r *http.Request) (verifyUpload, int, error) {
	return parseVerifyUploadWithTemp(w, r, func() (verifyManifestTemp, error) {
		return os.CreateTemp("", "shutterpipe-verify-manifest-*")
	})
}

func parseVerifyUploadWithTemp(
	w http.ResponseWriter,
	r *http.Request,
	createTemp func() (verifyManifestTemp, error),
) (verifyUpload, int, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxVerifyRequestBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		return verifyUpload{}, http.StatusBadRequest, fmt.Errorf("multipart/form-data is required: %w", err)
	}

	var upload verifyUpload
	cleanup := func() {
		if upload.manifestPath != "" {
			_ = os.Remove(upload.manifestPath)
			upload.manifestPath = ""
		}
	}
	configSeen := false
	manifestSeen := false
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			cleanup()
			return verifyUpload{}, multipartErrorStatus(nextErr), nextErr
		}
		name := part.FormName()
		switch name {
		case "config":
			if configSeen || part.FileName() != "" {
				_ = part.Close()
				cleanup()
				return verifyUpload{}, http.StatusBadRequest, errors.New("config field must appear exactly once")
			}
			configSeen = true
			data, readErr := io.ReadAll(io.LimitReader(part, maxVerifyConfigBytes+1))
			_ = part.Close()
			if readErr != nil {
				cleanup()
				return verifyUpload{}, multipartErrorStatus(readErr), readErr
			}
			if int64(len(data)) > maxVerifyConfigBytes {
				cleanup()
				return verifyUpload{}, http.StatusRequestEntityTooLarge, errors.New("config field is too large")
			}
			upload.configData = data
		case "hash_manifest":
			if manifestSeen || part.FileName() == "" {
				_ = part.Close()
				cleanup()
				return verifyUpload{}, http.StatusBadRequest, errors.New("hash_manifest file must appear at most once")
			}
			manifestSeen = true
			temp, createErr := createTemp()
			if createErr != nil {
				_ = part.Close()
				cleanup()
				return verifyUpload{}, http.StatusInternalServerError, createErr
			}
			upload.manifestPath = temp.Name()
			manifestWriter := &recordingWriter{writer: temp}
			written, copyErr := io.Copy(manifestWriter, io.LimitReader(part, maxHashManifestBytes+1))
			closeErr := temp.Close()
			_ = part.Close()
			if copyErr != nil {
				cleanup()
				status := multipartErrorStatus(copyErr)
				if manifestWriter.err != nil {
					status = http.StatusInternalServerError
				}
				return verifyUpload{}, status, copyErr
			}
			if closeErr != nil {
				cleanup()
				return verifyUpload{}, http.StatusInternalServerError, closeErr
			}
			if written == 0 {
				cleanup()
				return verifyUpload{}, http.StatusBadRequest, errors.New("hash_manifest must not be empty")
			}
			if written > maxHashManifestBytes {
				cleanup()
				return verifyUpload{}, http.StatusRequestEntityTooLarge, errors.New("hash_manifest exceeds 64MB")
			}
			upload.manifestName = filepath.Base(part.FileName())
		default:
			_ = part.Close()
			cleanup()
			return verifyUpload{}, http.StatusBadRequest, fmt.Errorf("unexpected multipart field: %s", name)
		}
	}
	if !configSeen || len(upload.configData) == 0 {
		cleanup()
		return verifyUpload{}, http.StatusBadRequest, errors.New("config field is required")
	}
	return upload, http.StatusOK, nil
}

func multipartErrorStatus(err error) int {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}
