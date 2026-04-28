package exec

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Result 描述一次命令执行的最小结果，用于封装到 result.chunk 中（汇总视角）。
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Chunk 描述流式执行过程中的单个结果分片，用于与 WS result.chunk 对齐。
//
// - Seq: 分片序号，从 1 开始递增；
// - StdoutChunk / StderrChunk: 本分片携带的 stdout/stderr 文本（固定大小块）；
// - ExitCode: 仅在 Final=true 的最后一个分片中设置退出码，其余分片为 nil；
// - Final: 是否为最后一个分片。
type Chunk struct {
	Seq         int
	StdoutChunk string
	StderrChunk string
	ExitCode    *int
	Final       bool
}

// Executor 定义命令执行接口，便于后续扩展不同执行策略（本地 shell、容器、沙箱等）。
//
// workDir 指定命令的工作目录：
//   - 空字符串：使用 Executor 实现的默认工作目录（ShellExecutor 为进程当前目录）；
//   - 支持以 "~" 开头的 $HOME 展开；
//   - 相对路径以进程当前目录为基准解析为绝对路径。
//
// Run 返回汇总结果；RunStream 以流式方式返回结果分片，直到通道被关闭。
type Executor interface {
	Run(ctx context.Context, command, workDir string, timeout time.Duration) (Result, error)
	RunStream(ctx context.Context, command, workDir string, timeout time.Duration) <-chan Chunk
}

// ShellExecutor 是一个最小实现：
// - 当 Enabled=false 时，不真正执行命令，而是返回占位结果；
// - 当 Enabled=true 时，使用本地 shell (sh -c) 执行命令。
//
// DefaultWorkDir 为每次命令执行的默认工作目录，当调用方未传入 workDir 时使用。
//
// TODO: 后续按安全策略接入白名单、沙箱、资源限制等能力。
type ShellExecutor struct {
	Enabled        bool
	DefaultWorkDir string
}

// resolveWorkDir 将给定 workDir 解析为可用于 cmd.Dir 的绝对路径：
//   - 按 override > fallback 的优先级选取；
//   - 空字符串直接返回 ""，由 os/exec 使用进程当前目录；
//   - 展开 "~" / "~/..." 为 $HOME；
//   - 相对路径转为绝对路径；
//   - 若路径不存在或不是目录，返回错误。
func resolveWorkDir(override, fallback string) (string, error) {
	raw := strings.TrimSpace(override)
	if raw == "" {
		raw = strings.TrimSpace(fallback)
	}
	if raw == "" {
		return "", nil
	}

	// 展开 ~ / ~/subdir
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		if raw == "~" {
			raw = home
		} else {
			raw = filepath.Join(home, raw[2:])
		}
	}

	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve workDir: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat workDir %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workDir %q is not a directory", abs)
	}
	return abs, nil
}

func (s ShellExecutor) Run(ctx context.Context, command, workDir string, timeout time.Duration) (Result, error) {
	if !s.Enabled {
		// 默认占位逻辑，避免误执行命令。
		return Result{
			ExitCode: 0,
			Stdout:   fmt.Sprintf("[shell disabled] would run: %s", command),
			Stderr:   "",
		}, nil
	}

	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	resolvedDir, err := resolveWorkDir(workDir, s.DefaultWorkDir)
	if err != nil {
		return Result{ExitCode: -1, Stderr: err.Error()}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = resolvedDir
	out, err := cmd.Output()
	res := Result{
		ExitCode: 0,
		Stdout:   string(out),
		Stderr:   "",
	}
	if err != nil {
		// 尝试从 ExitError 中取退出码。
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
			res.Stderr = string(exitErr.Stderr)
		} else {
			res.ExitCode = -1
			res.Stderr = err.Error()
		}
	}
	return res, nil
}

// RunStream 以流式方式执行命令，并通过只读通道返回 Chunk 序列。
//
// 行为约定：
//   - Enabled=false 时：返回单个占位 Chunk（Seq=1, Final=true, ExitCode=0）；
//   - Enabled=true 时：按固定大小块（默认 4KB）读取 stdout/stderr，分别生成 Chunk；
//     命令结束后追加最后一个 Final=true 的 Chunk，并携带 ExitCode。
//
// 超时处理：
//   - 使用独立的 timer 控制超时，不依赖 parent ctx；
//   - 超时后强制杀死整个进程组（避免 sh 子进程泄漏）；
//   - 保证 final chunk 一定会被发送，通道一定会被关闭。
func (s ShellExecutor) RunStream(ctx context.Context, command, workDir string, timeout time.Duration) <-chan Chunk {
	ch := make(chan Chunk)

	go func() {
		defer close(ch)

		// 占位模式：不真正执行命令，仅返回一条说明信息。
		if !s.Enabled {
			exitCode := 0
			ch <- Chunk{
				Seq:         1,
				StdoutChunk: fmt.Sprintf("[shell disabled] would run: %s", command),
				ExitCode:    &exitCode,
				Final:       true,
			}
			return
		}

		if timeout <= 0 {
			timeout = 30 * time.Second
		}

		resolvedDir, err := resolveWorkDir(workDir, s.DefaultWorkDir)
		if err != nil {
			exitCode := -1
			ch <- Chunk{
				Seq:         1,
				StderrChunk: err.Error(),
				ExitCode:    &exitCode,
				Final:       true,
			}
			return
		}

		// 使用独立 context，避免 parent ctx 取消直接影响命令生命周期。
		cmdCtx, cmdCancel := context.WithTimeout(context.Background(), timeout)
		defer cmdCancel()

		cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
		cmd.Dir = resolvedDir

		// 创建新进程组，确保超时后可以杀死整个进程树（包括子进程）。
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			ch <- Chunk{Seq: 1, StderrChunk: fmt.Sprintf("stdout pipe error: %v", err), Final: true}
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			ch <- Chunk{Seq: 1, StderrChunk: fmt.Sprintf("stderr pipe error: %v", err), Final: true}
			return
		}

		if err := cmd.Start(); err != nil {
			ch <- Chunk{Seq: 1, StderrChunk: fmt.Sprintf("start command error: %v", err), Final: true}
			return
		}

		// 超时后强制杀死进程组。
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		go func() {
			<-timer.C
			if cmd.Process != nil {
				// 杀死进程组（负 PID 表示进程组）。
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}()

		var (
			seq int32
			wg  sync.WaitGroup
		)

		// 按固定大小块读取 stdout。
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 4096)
			for {
				n, err := stdout.Read(buf)
				if n > 0 {
					chunk := string(buf[:n])
					seqN := int(atomic.AddInt32(&seq, 1))
					ch <- Chunk{Seq: seqN, StdoutChunk: chunk}
				}
				if err != nil {
					if err != io.EOF {
						seqN := int(atomic.AddInt32(&seq, 1))
						ch <- Chunk{Seq: seqN, StderrChunk: fmt.Sprintf("stdout read error: %v", err)}
					}
					break
				}
			}
		}()

		// 按固定大小块读取 stderr。
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 4096)
			for {
				n, err := stderr.Read(buf)
				if n > 0 {
					chunk := string(buf[:n])
					seqN := int(atomic.AddInt32(&seq, 1))
					ch <- Chunk{Seq: seqN, StderrChunk: chunk}
				}
				if err != nil {
					if err != io.EOF {
						seqN := int(atomic.AddInt32(&seq, 1))
						ch <- Chunk{Seq: seqN, StderrChunk: fmt.Sprintf("stderr read error: %v", err)}
					}
					break
				}
			}
		}()

		// 等待所有输出读取完毕。
		wg.Wait()

		// 获取退出码并发送最后一个分片。
		exitCode := 0
		if err := cmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				// 包含上下文超时在内的其他错误，统一视为 -1。
				exitCode = -1
			}
		}

		finalSeq := int(atomic.AddInt32(&seq, 1))
		ch <- Chunk{Seq: finalSeq, ExitCode: &exitCode, Final: true}
	}()

	return ch
}
