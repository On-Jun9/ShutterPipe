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
	VerifyStagedWithContext(context.Context, string, int64, []byte) error
}

type Copier struct {
	workers         int
	dryRun          bool
	hashVerify      bool
	partFileFactory func(string) (*os.File, error)
	verifier        stagedVerifier
}

func New(workers int, dryRun, hashVerify bool) *Copier {
	return &Copier{
		workers:         workers,
		dryRun:          dryRun,
		hashVerify:      hashVerify,
		partFileFactory: newPartFile,
		verifier:        verify.New(hashVerify),
	}
}

type CopyResult struct {
	Task               types.CopyTask
	VerifiedSourceHash []byte
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

	if err := ensureDirWithinRoot(task.DestinationRoot, filepath.Dir(task.DestPath)); err != nil {
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}

	partFileFactory := c.partFileFactory
	if partFileFactory == nil {
		partFileFactory = newPartFile
	}
	partFile, err := partFileFactory(task.DestPath)
	if err != nil {
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	partPath := partFile.Name()

	sourceHash, err := c.copyToPart(ctx, task.Source, partFile)
	if err != nil {
		if cleanupErr := os.Remove(partPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			err = errors.Join(err, cleanupErr)
		}
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	verifier := c.verifier
	if verifier == nil {
		verifier = verify.New(c.hashVerify)
	}
	if err := verifier.VerifyStagedWithContext(ctx, partPath, task.Source.Size, sourceHash); err != nil {
		if cleanupErr := os.Remove(partPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			err = errors.Join(err, cleanupErr)
		}
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	if c.hashVerify {
		if err := verify.New(true).VerifySourceHashWithContext(ctx, task.Source.Path, sourceHash); err != nil {
			if cleanupErr := os.Remove(partPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
				err = errors.Join(err, cleanupErr)
			}
			task.Status = types.TaskStatusFailed
			task.Error = err.Error()
			return CopyResult{Task: task, Error: err}
		}
	}
	if err := ctx.Err(); err != nil {
		if cleanupErr := os.Remove(partPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			err = errors.Join(err, cleanupErr)
		}
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}

	skipped, err := commitTask(partPath, &task)
	if err != nil {
		if cleanupErr := os.Remove(partPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			err = errors.Join(err, cleanupErr)
		}
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		return CopyResult{Task: task, Error: err}
	}
	if skipped {
		task.Status = types.TaskStatusSkipped
		return CopyResult{Task: task}
	}
	task.Status = types.TaskStatusCompleted
	return CopyResult{Task: task, VerifiedSourceHash: append([]byte(nil), sourceHash...)}
}

func newPartFile(finalDest string) (*os.File, error) {
	dir := filepath.Dir(finalDest)
	base := filepath.Base(finalDest)
	for attempt := 0; attempt < 100; attempt++ {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return nil, fmt.Errorf("failed to generate temporary filename: %w", err)
		}
		partPath := filepath.Join(dir, fmt.Sprintf(".%s.%x.part", base, suffix))
		file, err := os.OpenFile(partPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("failed to allocate temporary file for %s", finalDest)
}

func (c *Copier) copyToPart(ctx context.Context, source types.FileEntry, dstFile *os.File) ([]byte, error) {
	srcFile, err := os.Open(source.Path)
	if err != nil {
		dstFile.Close()
		return nil, err
	}
	defer srcFile.Close()
	before, err := srcFile.Stat()
	if err != nil {
		dstFile.Close()
		return nil, err
	}
	if before.Size() != source.Size || (!source.ModTime.IsZero() && !before.ModTime().Equal(source.ModTime)) {
		dstFile.Close()
		return nil, fmt.Errorf("source changed since scan: %s", source.Path)
	}
	partDest := dstFile.Name()
	hasher := sha256.New()

	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			dstFile.Close()
			return nil, err
		}

		n, readErr := srcFile.Read(buf)
		if n > 0 {
			written, err := dstFile.Write(buf[:n])
			if err != nil {
				dstFile.Close()
				return nil, err
			}
			if written != n {
				dstFile.Close()
				return nil, io.ErrShortWrite
			}
			if c.hashVerify {
				if _, err := hasher.Write(buf[:n]); err != nil {
					dstFile.Close()
					return nil, err
				}
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			dstFile.Close()
			return nil, readErr
		}
	}
	after, err := srcFile.Stat()
	if err != nil {
		dstFile.Close()
		return nil, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		dstFile.Close()
		return nil, fmt.Errorf("source changed during copy: %s", source.Path)
	}

	if err := dstFile.Close(); err != nil {
		return nil, err
	}

	// Preserve modification time
	os.Chtimes(partDest, before.ModTime(), before.ModTime())

	if !c.hashVerify {
		return nil, nil
	}
	return hasher.Sum(nil), nil
}

func commitTask(partPath string, task *types.CopyTask) (bool, error) {
	if task.Action == types.CopyActionOverwritten {
		return false, replaceFile(partPath, task.DestPath)
	}

	if err := movePartNoReplace(partPath, task.DestPath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrExist) {
		return false, err
	}

	switch task.ConflictPolicy {
	case types.ConflictPolicySkip:
		if err := os.Remove(partPath); err != nil {
			return false, err
		}
		task.Action = types.CopyActionSkipped
		return true, nil

	case types.ConflictPolicyOverwrite:
		if err := replaceFile(partPath, task.DestPath); err != nil {
			return false, err
		}
		task.Action = types.CopyActionOverwritten
		return false, nil

	case types.ConflictPolicyRename:
		task.Action = types.CopyActionRenamed
		return false, commitWithUniqueName(partPath, task, task.DestPath)

	case types.ConflictPolicyQuarantine:
		if err := ensureDirWithinRoot(task.DestinationRoot, task.QuarantineDir); err != nil {
			return false, err
		}
		quarantineDest := filepath.Join(task.QuarantineDir, task.Source.Name)
		task.Action = types.CopyActionQuarantined
		if err := movePartNoReplace(partPath, quarantineDest); err == nil {
			task.DestPath = quarantineDest
			return false, nil
		} else if !errors.Is(err, os.ErrExist) {
			return false, err
		}
		return false, commitWithUniqueName(partPath, task, quarantineDest)

	default:
		return false, os.ErrExist
	}
}

func ensureDirWithinRoot(root, path string) error {
	if root == "" {
		return os.MkdirAll(path, 0755)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("failed to resolve destination root: %w", err)
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve destination path: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("destination path escapes configured root: %s", path)
	}

	// Creating the configured root itself is authorized. Descendants are then
	// created one component at a time, validating every existing symlink before
	// any deeper directory can be created through it.
	if err := os.MkdirAll(rootAbs, 0755); err != nil {
		return err
	}
	if err := ensureResolvedWithinRoot(rootAbs, rootAbs); err != nil {
		return err
	}
	if rel == "." {
		return nil
	}

	current := rootAbs
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if mkdirErr := os.Mkdir(current, 0755); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				return mkdirErr
			}
		} else if statErr != nil {
			return statErr
		} else if info.Mode().IsRegular() {
			return fmt.Errorf("destination path component is not a directory: %s", current)
		}
		if err := ensureResolvedWithinRoot(rootAbs, current); err != nil {
			return err
		}
		resolvedInfo, statErr := os.Stat(current)
		if statErr != nil {
			return statErr
		}
		if !resolvedInfo.IsDir() {
			return fmt.Errorf("destination path component is not a directory: %s", current)
		}
	}
	return nil
}

func ensureResolvedWithinRoot(root, path string) error {
	if root == "" {
		return nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("failed to resolve destination root: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("failed to resolve destination path: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("destination path escapes configured root: %s", path)
	}
	return nil
}

func commitWithUniqueName(partPath string, task *types.CopyTask, basePath string) error {
	for i := 1; i < 10000; i++ {
		candidate := uniqueCommitCandidate(basePath, i)
		if err := movePartNoReplace(partPath, candidate); err == nil {
			task.DestPath = candidate
			return nil
		} else if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	return fmt.Errorf("no available destination name for %s", basePath)
}

func movePartNoReplace(partPath, finalDest string) error {
	if err := renameNoReplace(partPath, finalDest); err == nil || errors.Is(err, os.ErrExist) {
		return err
	}

	// Some filesystems do not implement the platform's exclusive rename. A
	// same-directory hard link still publishes the completed part atomically.
	if linkErr := os.Link(partPath, finalDest); linkErr == nil {
		if removeErr := os.Remove(partPath); removeErr != nil {
			rollbackErr := os.Remove(finalDest)
			return errors.Join(removeErr, rollbackErr)
		}
		return nil
	} else if errors.Is(linkErr, os.ErrExist) {
		return linkErr
	} else {
		return fmt.Errorf("destination filesystem does not support atomic no-replace commit: %w", linkErr)
	}
}

func uniqueCommitCandidate(path string, index int) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, index, ext))
}
