package copier

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type Copier struct {
	workers    int
	dryRun     bool
	hashVerify bool
}

func New(workers int, dryRun, hashVerify bool) *Copier {
	return &Copier{
		workers:    workers,
		dryRun:     dryRun,
		hashVerify: hashVerify,
	}
}

type CopyResult struct {
	Task  types.CopyTask
	Error error
}

func (c *Copier) CopyAll(ctx context.Context, tasks []types.CopyTask, resultChan chan<- CopyResult) {
	if ctx == nil {
		ctx = context.Background()
	}

	if ctx.Err() != nil {
		close(resultChan)
		return
	}

	workers := c.workers
	if workers < 1 {
		workers = 1
	}

	taskChan := make(chan types.CopyTask)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case task, ok := <-taskChan:
					if !ok {
						return
					}
					result := c.copyOne(ctx, task)
					resultChan <- result
				}
			}
		}()
	}

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			close(taskChan)
			wg.Wait()
			close(resultChan)
			return
		case taskChan <- task:
		}
	}
	close(taskChan)

	wg.Wait()
	close(resultChan)
}

func (c *Copier) copyOne(ctx context.Context, task types.CopyTask) CopyResult {
	if err := ctx.Err(); err != nil {
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}

	if c.dryRun {
		task.Status = types.TaskStatusCompleted
		task.Action = types.CopyActionCopied
		return CopyResult{Task: task}
	}

	if err := os.MkdirAll(filepath.Dir(task.DestPath), 0755); err != nil {
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}

	partPath := task.DestPath + ".part"

	if err := c.atomicCopy(ctx, task.Source.Path, partPath, task.DestPath); err != nil {
		os.Remove(partPath)
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}

	task.Status = types.TaskStatusCompleted
	return CopyResult{Task: task}
}

func (c *Copier) atomicCopy(ctx context.Context, src, partDest, finalDest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(partDest)
	if err != nil {
		return err
	}

	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			dstFile.Close()
			return err
		}

		n, readErr := srcFile.Read(buf)
		if n > 0 {
			if _, err := dstFile.Write(buf[:n]); err != nil {
				dstFile.Close()
				return err
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			dstFile.Close()
			return readErr
		}
	}

	if err := dstFile.Close(); err != nil {
		return err
	}

	// Preserve modification time
	info, err := srcFile.Stat()
	if err == nil {
		os.Chtimes(partDest, info.ModTime(), info.ModTime())
	}

	return os.Rename(partDest, finalDest)
}
