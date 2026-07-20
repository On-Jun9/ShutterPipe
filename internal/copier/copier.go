package copier

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/On-Jun9/ShutterPipe/internal/verify"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type stagedVerifier interface {
	VerifyStagedFileWithContext(context.Context, *os.File, int64, []byte) error
}

type Copier struct {
	workers                 int
	dryRun                  bool
	hashVerify              bool
	partFileFactory         func(string) (*os.File, error)
	beforeDestinationCreate func(types.CopyTask)
	verifier                stagedVerifier
}

func New(workers int, dryRun, hashVerify bool) *Copier {
	return &Copier{
		workers:    workers,
		dryRun:     dryRun,
		hashVerify: hashVerify,
		verifier:   verify.New(hashVerify),
	}
}

type CopyResult struct {
	Task               types.CopyTask
	VerifiedSourceHash []byte
	Warning            string
	Error              error
}

func (c *Copier) CopyAll(ctx context.Context, tasks []types.CopyTask, resultChan chan<- CopyResult) {
	if ctx == nil {
		ctx = context.Background()
	}

	if ctx.Err() != nil {
		close(resultChan)
		return
	}
	if len(tasks) == 0 {
		close(resultChan)
		return
	}

	workers := c.workers
	if workers < 1 {
		workers = 1
	} else if workers > len(tasks) {
		workers = len(tasks)
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
		if task.Action == "" {
			task.Action = types.CopyActionCopied
		}
		return CopyResult{Task: task}
	}

	rootPath := task.DestinationRoot
	if rootPath == "" {
		rootPath = filepath.Dir(task.DestPath)
	}
	rootPath, err := filepath.Abs(rootPath)
	if err != nil {
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	if err := os.MkdirAll(rootPath, 0755); err != nil {
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	destRoot, err := os.OpenRoot(rootPath)
	if err != nil {
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	defer destRoot.Close()
	destRel, err := relativeDestinationPath(rootPath, task.DestPath)
	if err != nil {
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	if c.beforeDestinationCreate != nil {
		c.beforeDestinationCreate(task)
	}
	if err := destRoot.MkdirAll(filepath.Dir(destRel), 0755); err != nil {
		err = fmt.Errorf("destination path escapes configured root or cannot be created: %s: %w", task.DestPath, err)
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}

	var partFile *os.File
	var partRel string
	if c.partFileFactory != nil {
		partFile, err = c.partFileFactory(task.DestPath)
		if err == nil {
			partRel, err = relativeDestinationPath(rootPath, partFile.Name())
		}
	} else {
		partFile, partRel, err = newPartFileInRoot(destRoot, destRel)
	}
	if err != nil {
		if partFile != nil {
			_ = partFile.Close()
		}
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}

	sourceHash, warning, err := c.copyToPart(ctx, task.Source, partFile)
	if err != nil {
		err = joinRootCleanupError(destRoot, partRel, err)
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	verifier := c.verifier
	if verifier == nil {
		verifier = verify.New(c.hashVerify)
	}
	stagedFile, err := destRoot.Open(partRel)
	if err != nil {
		err = joinRootCleanupError(destRoot, partRel, err)
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	verifyErr := verifier.VerifyStagedFileWithContext(ctx, stagedFile, task.Source.Size, sourceHash)
	closeErr := stagedFile.Close()
	if verifyErr != nil || closeErr != nil {
		err = errors.Join(verifyErr, closeErr)
		err = joinRootCleanupError(destRoot, partRel, err)
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	if c.hashVerify {
		if err := verify.New(true).VerifySourceHashWithContext(ctx, task.Source.Path, sourceHash); err != nil {
			err = joinRootCleanupError(destRoot, partRel, err)
			task.Status = types.TaskStatusFailed
			task.Error = err.Error()
			return CopyResult{Task: task, Error: err}
		}
	}
	if err := ctx.Err(); err != nil {
		err = joinRootCleanupError(destRoot, partRel, err)
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}

	skipped, err := commitTaskInRoot(destRoot, rootPath, partRel, destRel, &task)
	if err != nil {
		err = joinRootCleanupError(destRoot, partRel, err)
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	if skipped {
		task.Status = types.TaskStatusSkipped
		return CopyResult{Task: task, Warning: warning}
	}
	task.Status = types.TaskStatusCompleted
	return CopyResult{Task: task, VerifiedSourceHash: append([]byte(nil), sourceHash...), Warning: warning}
}

func newPartFileInRoot(root *os.Root, finalDest string) (*os.File, string, error) {
	dir := filepath.Dir(finalDest)
	base := filepath.Base(finalDest)
	for attempt := 0; attempt < 100; attempt++ {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return nil, "", fmt.Errorf("failed to generate temporary filename: %w", err)
		}
		partPath := filepath.Join(dir, fmt.Sprintf(".%s.%x.part", base, suffix))
		file, err := root.OpenFile(partPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
		if err == nil {
			return file, partPath, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("failed to allocate temporary file for %s", finalDest)
}

func relativeDestinationPath(rootPath, path string) (string, error) {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootPath, pathAbs)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("destination path escapes configured root: %s", path)
	}
	return rel, nil
}

func joinRootCleanupError(root *os.Root, partPath string, original error) error {
	if cleanupErr := root.Remove(partPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
		return errors.Join(original, cleanupErr)
	}
	return original
}

func (c *Copier) copyToPart(ctx context.Context, source types.FileEntry, dstFile *os.File) ([]byte, string, error) {
	srcFile, err := os.Open(source.Path)
	if err != nil {
		dstFile.Close()
		return nil, "", err
	}
	defer srcFile.Close()
	before, err := srcFile.Stat()
	if err != nil {
		dstFile.Close()
		return nil, "", err
	}
	if before.Size() != source.Size || (!source.ModTime.IsZero() && !before.ModTime().Equal(source.ModTime)) {
		dstFile.Close()
		return nil, "", fmt.Errorf("source changed since scan: %s", source.Path)
	}
	hasher := sha256.New()

	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			dstFile.Close()
			return nil, "", err
		}

		n, readErr := srcFile.Read(buf)
		if n > 0 {
			written, err := dstFile.Write(buf[:n])
			if err != nil {
				dstFile.Close()
				return nil, "", err
			}
			if written != n {
				dstFile.Close()
				return nil, "", io.ErrShortWrite
			}
			if c.hashVerify {
				if _, err := hasher.Write(buf[:n]); err != nil {
					dstFile.Close()
					return nil, "", err
				}
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			dstFile.Close()
			return nil, "", readErr
		}
	}
	after, err := srcFile.Stat()
	if err != nil {
		dstFile.Close()
		return nil, "", err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		dstFile.Close()
		return nil, "", fmt.Errorf("source changed during copy: %s", source.Path)
	}

	var warning string
	if err := setFileTimes(dstFile, before.ModTime()); err != nil {
		warning = fmt.Sprintf("수정 시각을 보존하지 못했습니다 %s: %v", source.Path, err)
	}
	if err := dstFile.Close(); err != nil {
		return nil, warning, err
	}

	if !c.hashVerify {
		return nil, warning, nil
	}
	return hasher.Sum(nil), warning, nil
}

func commitTaskInRoot(root *os.Root, rootPath, partPath, destPath string, task *types.CopyTask) (bool, error) {
	if task.ConflictPolicy == types.ConflictPolicyOverwrite {
		if _, statErr := root.Stat(destPath); statErr == nil {
			task.Action = types.CopyActionOverwritten
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return false, statErr
		}
		return false, root.Rename(partPath, destPath)
	}

	if err := movePartNoReplaceInRoot(root, partPath, destPath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrExist) {
		return false, err
	}

	switch task.ConflictPolicy {
	case types.ConflictPolicySkip:
		if err := root.Remove(partPath); err != nil {
			return false, err
		}
		task.Action = types.CopyActionSkipped
		return true, nil
	case types.ConflictPolicyRename:
		task.Action = types.CopyActionRenamed
		return false, commitWithUniqueNameInRoot(root, rootPath, partPath, task, destPath)
	case types.ConflictPolicyQuarantine:
		quarantinePath, err := relativeDestinationPath(rootPath, task.QuarantineDir)
		if err != nil {
			return false, err
		}
		if err := root.MkdirAll(quarantinePath, 0755); err != nil {
			return false, fmt.Errorf("quarantine path escapes configured root or cannot be created: %s: %w", task.QuarantineDir, err)
		}
		quarantineDest := filepath.Join(quarantinePath, task.Source.Name)
		task.Action = types.CopyActionQuarantined
		if err := movePartNoReplaceInRoot(root, partPath, quarantineDest); err == nil {
			task.DestPath = filepath.Join(rootPath, quarantineDest)
			return false, nil
		} else if !errors.Is(err, os.ErrExist) {
			return false, err
		}
		return false, commitWithUniqueNameInRoot(root, rootPath, partPath, task, quarantineDest)
	default:
		return false, os.ErrExist
	}
}

func commitWithUniqueNameInRoot(root *os.Root, rootPath, partPath string, task *types.CopyTask, basePath string) error {
	for i := 1; i < 10000; i++ {
		candidate := uniqueCommitCandidate(basePath, i)
		if err := movePartNoReplaceInRoot(root, partPath, candidate); err == nil {
			task.DestPath = filepath.Join(rootPath, candidate)
			return nil
		} else if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	return fmt.Errorf("no available destination name for %s", basePath)
}

func movePartNoReplaceInRoot(root *os.Root, partPath, finalDest string) error {
	if err := root.Link(partPath, finalDest); err != nil {
		return err
	}
	if err := root.Remove(partPath); err != nil {
		rollbackErr := root.Remove(finalDest)
		return errors.Join(err, rollbackErr)
	}
	return nil
}

func uniqueCommitCandidate(path string, index int) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, index, ext))
}
