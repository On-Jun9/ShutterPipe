package pipeline

import "github.com/On-Jun9/ShutterPipe/pkg/types"

type ProgressCallback func(update ProgressUpdate)

type ProgressUpdate struct {
	Type     string        `json:"type"`
	Kind     types.RunKind `json:"kind,omitempty"`
	RunID    string        `json:"run_id,omitempty"`
	ServerID string        `json:"server_id,omitempty"`
	Revision uint64        `json:"revision,omitempty"`
	Message  string        `json:"message,omitempty"`
	// omitempty를 쓰면 진행 0건(분석 첫 이벤트)에서 필드가 빠져 클라이언트가
	// 진행률을 NaN으로 계산한다. 진행 카운터는 항상 값을 보낸다.
	Current       int                  `json:"current"`
	Total         int                  `json:"total"`
	Filename      string               `json:"filename,omitempty"`
	Action        types.CopyAction     `json:"action,omitempty"`
	VerifyVerdict types.VerifyVerdict  `json:"verify_verdict,omitempty"`
	Summary       *types.RunSummary    `json:"summary,omitempty"`
	VerifySummary *types.VerifySummary `json:"verify_summary,omitempty"`
	Error         string               `json:"error,omitempty"`
}
