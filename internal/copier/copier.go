package copier

import (
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

func (c *Copier) CopyAll(tasks []types.CopyTask, resultChan chan<- CopyResult) {
	taskChan := make(chan types.CopyTask, len(tasks))

	var wg sync.WaitGroup
	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				result := c.copyOne(task)
				resultChan <- result
			}
		}()
	}

	for _, task := range tasks {
		taskChan <- task
	}
	close(taskChan)

	wg.Wait()
	close(resultChan)
}

func (c *Copier) copyOne(task types.CopyTask) CopyResult {
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

	if err := c.atomicCopy(task.Source.Path, partPath, task.DestPath); err != nil {
		os.Remove(partPath)
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}

	task.Status = types.TaskStatusCompleted
	return CopyResult{Task: task}
}

func (c *Copier) atomicCopy(src, partDest, finalDest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(partDest)
	if err != nil {
		return err
	}

	_, err = io.Copy(dstFile, srcFile)
	if closeErr := dstFile.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	// Preserve modification time
	info, err := srcFile.Stat()
	if err == nil {
		os.Chtimes(partDest, info.ModTime(), info.ModTime())
	}

	return os.Rename(partDest, finalDest)
}
