package pipeline

import "github.com/On-Jun9/ShutterPipe/pkg/types"

type ProgressCallback func(update ProgressUpdate)

type ProgressUpdate struct {
	Type     string            `json:"type"`
	Message  string            `json:"message,omitempty"`
	Current  int               `json:"current,omitempty"`
	Total    int               `json:"total,omitempty"`
	Filename string            `json:"filename,omitempty"`
	Action   types.CopyAction  `json:"action,omitempty"`
	Summary  *types.RunSummary `json:"summary,omitempty"`
	Error    string            `json:"error,omitempty"`
}
