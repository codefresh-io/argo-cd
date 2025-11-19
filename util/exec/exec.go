package exec

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/argoproj/gitops-engine/pkg/utils/tracing"
	"github.com/sirupsen/logrus"

	"github.com/argoproj/argo-cd/v3/util/log"
	"github.com/argoproj/argo-cd/v3/util/rand"
)

var (
	timeout      time.Duration
	fatalTimeout time.Duration
	Unredacted   = Redact(nil)
)

type ExecRunOpts struct {
	// Redactor redacts tokens from the output
	Redactor func(text string) string
	// TimeoutBehavior configures what to do in case of timeout
	TimeoutBehavior TimeoutBehavior
	// SkipErrorLogging determines whether to skip logging of execution errors (rc > 0)
	SkipErrorLogging bool
	// CaptureStderr determines whether to capture stderr in addition to stdout
	CaptureStderr bool
}

func init() {
	initTimeout()
}

func initTimeout() {
	var err error
	timeout, err = time.ParseDuration(os.Getenv("ARGOCD_EXEC_TIMEOUT"))
	if err != nil {
		timeout = 90 * time.Second
	}
	fatalTimeout, err = time.ParseDuration(os.Getenv("ARGOCD_EXEC_FATAL_TIMEOUT"))
	if err != nil {
		fatalTimeout = 10 * time.Second
	}
}

func Run(cmd *exec.Cmd) (string, error) {
	return RunWithRedactor(cmd, nil)
}

func RunWithRedactor(cmd *exec.Cmd, redactor func(text string) string) (string, error) {
	opts := ExecRunOpts{Redactor: redactor}
	return RunWithExecRunOpts(cmd, opts)
}

func RunWithExecRunOpts(cmd *exec.Cmd, opts ExecRunOpts) (string, error) {
	cmdOpts := CmdOpts{Timeout: timeout, FatalTimeout: fatalTimeout, Redactor: opts.Redactor, TimeoutBehavior: opts.TimeoutBehavior, SkipErrorLogging: opts.SkipErrorLogging}
	span := tracing.NewLoggingTracer(log.NewLogrusLogger(log.NewWithCurrentConfig())).StartSpan(fmt.Sprintf("exec %v", cmd.Args[0]))
	span.SetBaggageItem("dir", cmd.Dir)
	if cmdOpts.Redactor != nil {
		span.SetBaggageItem("args", opts.Redactor(fmt.Sprintf("%v", cmd.Args)))
	} else {
		span.SetBaggageItem("args", fmt.Sprintf("%v", cmd.Args))
	}
	defer span.Finish()
	return RunCommandExt(cmd, cmdOpts)
}

// GetCommandArgsToLog represents the given command in a way that we can copy-and-paste into a terminal
func GetCommandArgsToLog(cmd *exec.Cmd) string {
	var argsToLog []string
	for _, arg := range cmd.Args {
		if arg == "" {
			argsToLog = append(argsToLog, `""`)
			continue
		}

		containsSpace := false
		for _, r := range arg {
			if unicode.IsSpace(r) {
				containsSpace = true
				break
			}
		}
		if containsSpace {
			// add quotes and escape any internal quotes
			argsToLog = append(argsToLog, strconv.Quote(arg))
		} else {
			argsToLog = append(argsToLog, arg)
		}
	}
	args := strings.Join(argsToLog, " ")
	return args
}

type CmdError struct {
	Args   string
	Stderr string
	Cause  error
}

func (ce *CmdError) Error() string {
	res := fmt.Sprintf("`%v` failed %v", ce.Args, ce.Cause)
	if ce.Stderr != "" {
		res = fmt.Sprintf("%s: %s", res, ce.Stderr)
	}
	return res
}

func (ce *CmdError) String() string {
	return ce.Error()
}

func newCmdError(args string, cause error, stderr string) *CmdError {
	return &CmdError{Args: args, Stderr: stderr, Cause: cause}
}

// TimeoutBehavior defines behavior for when the command takes longer than the passed in timeout to exit
// By default, SIGKILL is sent to the process and it is not waited upon
type TimeoutBehavior struct {
	// Signal determines the signal to send to the process
	Signal syscall.Signal
	// ShouldWait determines whether to wait for the command to exit once timeout is reached
	ShouldWait bool
}

type CmdOpts struct {
	// Timeout determines how long to wait for the command to exit
	Timeout time.Duration
	// FatalTimeout is the amount of additional time to wait after Timeout before fatal SIGKILL
	FatalTimeout time.Duration
	// Redactor redacts tokens from the output
	Redactor func(text string) string
	// TimeoutBehavior configures what to do in case of timeout
	TimeoutBehavior TimeoutBehavior
	// SkipErrorLogging defines whether to skip logging of execution errors (rc > 0)
	SkipErrorLogging bool
	// CaptureStderr defines whether to capture stderr in addition to stdout
	CaptureStderr bool
}

var DefaultCmdOpts = CmdOpts{
	Timeout:          time.Duration(0),
	FatalTimeout:     time.Duration(0),
	Redactor:         Unredacted,
	TimeoutBehavior:  TimeoutBehavior{syscall.SIGKILL, false},
	SkipErrorLogging: false,
	CaptureStderr:    false,
}

func Redact(items []string) func(text string) string {
	return func(text string) string {
		for _, item := range items {
			text = strings.ReplaceAll(text, item, "******")
		}
		return text
	}
}

// RunCommandExt is a convenience function to run/log a command and return/log stderr in an error upon
// failure.
func RunCommandExt(cmd *exec.Cmd, opts CmdOpts) (string, error) {
	execId, err := rand.RandHex(5)
	if err != nil {
		return "", err
	}
	logCtx := logrus.WithFields(logrus.Fields{"execID": execId})

	redactor := DefaultCmdOpts.Redactor
	if opts.Redactor != nil {
		redactor = opts.Redactor
	}

	// log in a way we can copy-and-paste into a terminal
	args := strings.Join(cmd.Args, " ")
	logCtx.WithFields(logrus.Fields{"dir": cmd.Dir}).Info(redactor(args))

	// Best-effort cleanup of a stale HEAD.lock after the command finishes.
	defer func() {
		if cmd.Dir == "" {
			return
		}
		lockPath := filepath.Join(cmd.Dir, ".git", "HEAD.lock")
		logCtx.WithFields(logrus.Fields{"headLockPath": lockPath}).Info("Checking HEAD.lock presence post-exec")
		if _, err := os.Stat(lockPath); err == nil {
			// Log and attempt removal; ignore ENOENT races
			logCtx.WithFields(logrus.Fields{"headLockPath": lockPath}).Warn("HEAD.lock present post-exec, removing it")
			if rmErr := os.Remove(lockPath); rmErr != nil && !os.IsNotExist(rmErr) {
				logCtx.WithFields(logrus.Fields{"headLockPath": lockPath}).Warnf("Failed to remove HEAD.lock: %v", rmErr)
			}
		} else if !os.IsNotExist(err) {
			logCtx.WithFields(logrus.Fields{"headLockPath": lockPath}).Warnf("Failed to stat HEAD.lock: %v", err)
		} else {
			logCtx.WithFields(logrus.Fields{"headLockPath": lockPath}).Info("HEAD.lock not present post-exec")
		}
	}()

	// Helper: debug whether HEAD.lock exists under the current working directory
	logHeadLockStatus := func(where string) {
		if cmd.Dir == "" {
			return
		}
		lockPath := filepath.Join(cmd.Dir, ".git", "HEAD.lock")
		fileInfo, statErr := os.Stat(lockPath)
		exists := statErr == nil
		fields := logrus.Fields{
			"headLockPath":   lockPath,
			"headLockExists": exists,
			"where":          where,
		}

		pgid, pgErr := syscall.Getpgid(cmd.Process.Pid)
		if pgErr == nil && pgid > 0 {
			// Portable ps: list all processes, print needed columns without headers, then filter by PGID in Go.
			out, _ := exec.Command(
				"ps",
				"-ax",
				"-o", "pid=,ppid=,pgid=,etime=,comm=,args=",
			).Output()
			if len(out) > 0 {
				var b strings.Builder
				want := strconv.Itoa(pgid)
				for _, line := range strings.Split(string(out), "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					fieldsSlice := strings.Fields(line)
					if len(fieldsSlice) < 3 {
						continue
					}
					if fieldsSlice[2] == want {
						b.WriteString(line)
						b.WriteByte('\n')
					}
				}
				fields["gitProcsInGroup"] = strings.TrimSpace(b.String())
			}
		}

		if exists {
			fields["headLockSize"] = fileInfo.Size()
			fields["headLockMode"] = fileInfo.Mode().String()
			fields["headLockModTime"] = fileInfo.ModTime()
			fields["headLockIsDir"] = fileInfo.IsDir()

		}
		logCtx.WithFields(fields).Info("HEAD.lock status")
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Configure the child to run in its own process group so we can signal the whole group on timeout/cancel.
	// On Unix this sets Setpgid; on Windows this is a no-op.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = newSysProcAttr(true)
	}

	start := time.Now()
	err = cmd.Start()
	if err != nil {
		return "", err
	}

	logHeadLockStatus("start-exec")

	done := make(chan error)
	go func() { done <- cmd.Wait() }()

	// Start timers for timeout
	timeout := DefaultCmdOpts.Timeout
	fatalTimeout := DefaultCmdOpts.FatalTimeout

	if opts.Timeout != time.Duration(0) {
		timeout = opts.Timeout
	}

	if opts.FatalTimeout != time.Duration(0) {
		fatalTimeout = opts.FatalTimeout
	}

	var timoutCh <-chan time.Time
	if timeout != 0 {
		timoutCh = time.NewTimer(timeout).C
	}

	var fatalTimeoutCh <-chan time.Time
	if fatalTimeout != 0 {
		fatalTimeoutCh = time.NewTimer(timeout + fatalTimeout).C
	}

	timeoutBehavior := DefaultCmdOpts.TimeoutBehavior
	fatalTimeoutBehaviour := syscall.SIGKILL
	if opts.TimeoutBehavior.Signal != syscall.Signal(0) {
		timeoutBehavior = opts.TimeoutBehavior
	}

	select {
	// noinspection ALL
	case <-timoutCh:
		// send timeout signal
		// signal the process group (negative PID) so children are terminated as well
		if cmd.Process != nil {
			_ = sysCallSignal(-cmd.Process.Pid, timeoutBehavior.Signal)
		}
		// wait on timeout signal and fallback to fatal timeout signal
		if timeoutBehavior.ShouldWait {
			select {
			case <-done:
				logHeadLockStatus("timeout-waited-done")
			case <-fatalTimeoutCh:
				// upgrades to fatal signal (default SIGKILL) if cmd does not respect the initial signal
				if cmd.Process != nil {
					_ = sysCallSignal(-cmd.Process.Pid, fatalTimeoutBehaviour)
				}
				// now original cmd should exit immediately after SIGKILL
				<-done
				// return error with a marker indicating that cmd exited only after fatal SIGKILL
				output := stdout.String()
				if opts.CaptureStderr {
					output += stderr.String()
				}
				logCtx.WithFields(logrus.Fields{"duration": time.Since(start)}).Debug(redactor(output))
				logHeadLockStatus("fatal-timeout")
				err = newCmdError(redactor(args), fmt.Errorf("fatal timeout after %v", timeout+fatalTimeout), "")
				logCtx.Error(err.Error())
				return strings.TrimSuffix(output, "\n"), err
			}
		}
		// either did not wait for timeout or cmd did respect SIGTERM
		output := stdout.String()
		if opts.CaptureStderr {
			output += stderr.String()
		}
		logCtx.WithFields(logrus.Fields{"duration": time.Since(start)}).Debug(redactor(output))
		logHeadLockStatus("timeout")
		err = newCmdError(redactor(args), fmt.Errorf("timeout after %v", timeout), "")
		logCtx.Error(err.Error())
		return strings.TrimSuffix(output, "\n"), err
	case err := <-done:
		if err != nil {
			output := stdout.String()
			if opts.CaptureStderr {
				output += stderr.String()
			}
			logCtx.WithFields(logrus.Fields{"duration": time.Since(start)}).Debug(redactor(output))
			err := newCmdError(redactor(args), errors.New(redactor(err.Error())), strings.TrimSpace(redactor(stderr.String())))
			if !opts.SkipErrorLogging {
				logCtx.Error(err.Error())
			}
			logHeadLockStatus("done-error")
			return strings.TrimSuffix(output, "\n"), err
		}
	}
	output := stdout.String()
	if opts.CaptureStderr {
		output += stderr.String()
	}
	logCtx.WithFields(logrus.Fields{"duration": time.Since(start)}).Debug(redactor(output))
	logHeadLockStatus("done-success")

	return strings.TrimSuffix(output, "\n"), nil
}

func RunCommand(name string, opts CmdOpts, arg ...string) (string, error) {
	return RunCommandExt(exec.Command(name, arg...), opts)
}
