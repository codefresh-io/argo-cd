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
	"sync"
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

	// Capture process group information while processes are running
	var capturedProcessInfo []string
	var capturedProcessMutex sync.Mutex

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
		if exists {
			fields["headLockSize"] = fileInfo.Size()
			fields["headLockMode"] = fileInfo.Mode().String()
			fields["headLockModTime"] = fileInfo.ModTime()
			fields["headLockIsDir"] = fileInfo.IsDir()
		}

		// Add process group information if the process has started
		if cmd.Process != nil {
			pgid := cmd.Process.Pid // Process group ID is the same as the main process PID when Setpgid=true
			fields["processGroupId"] = pgid

			// Try to get current process group info
			currentProcesses := getProcessGroupInfo(pgid)
			if len(currentProcesses) > 0 && !strings.Contains(currentProcesses[0], "terminated or no processes found") {
				fields["processGroupProcesses"] = currentProcesses
				// Update captured info if we got fresh data
				capturedProcessMutex.Lock()
				capturedProcessInfo = currentProcesses
				capturedProcessMutex.Unlock()

				// Check which processes might be related to the lock file
				if exists {
					lockDir := filepath.Dir(lockPath)
					suspiciousProcesses := findProcessesInDirectory(currentProcesses, lockDir)
					if len(suspiciousProcesses) > 0 {
						fields["processesInLockDirectory"] = suspiciousProcesses
					}
				}
			} else {
				capturedProcessMutex.Lock()
				if len(capturedProcessInfo) > 0 {
					// Use previously captured info if current query failed
					fields["processGroupProcesses"] = capturedProcessInfo
					fields["processGroupProcessesNote"] = "captured during execution (process group has since terminated)"

					// Check captured processes for lock file relation
					if exists {
						lockDir := filepath.Dir(lockPath)
						suspiciousProcesses := findProcessesInDirectory(capturedProcessInfo, lockDir)
						if len(suspiciousProcesses) > 0 {
							fields["processesInLockDirectory"] = suspiciousProcesses
							fields["processesInLockDirectoryNote"] = "from captured process info"
						}
					}
				} else {
					fields["processGroupProcesses"] = currentProcesses
				}
				capturedProcessMutex.Unlock()
			}
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
		// Capture process group info RIGHT BEFORE sending timeout signal
		if cmd.Process != nil {
			pgid := cmd.Process.Pid
			preTerminationProcesses := getProcessGroupInfo(pgid)
			if len(preTerminationProcesses) > 0 && !strings.Contains(preTerminationProcesses[0], "terminated or no processes found") {
				capturedProcessMutex.Lock()
				capturedProcessInfo = preTerminationProcesses
				capturedProcessMutex.Unlock()
				logCtx.WithFields(logrus.Fields{
					"processGroupId":        pgid,
					"processGroupProcesses": preTerminationProcesses,
					"capturePoint":          "pre-timeout-signal",
				}).Info("Process group info captured before timeout signal")
			}
		}
		// send timeout signal
		// signal the process group (negative PID) so children are terminated as well
		if cmd.Process != nil {
			_ = sysCallSignal(-cmd.Process.Pid, timeoutBehavior.Signal)
		}
		// wait on timeout signal and fallback to fatal timeout signal
		if timeoutBehavior.ShouldWait {
			select {
			case <-done:
			case <-fatalTimeoutCh:
				// Capture process group info RIGHT BEFORE sending fatal signal
				if cmd.Process != nil {
					pgid := cmd.Process.Pid
					preFatalProcesses := getProcessGroupInfo(pgid)
					if len(preFatalProcesses) > 0 && !strings.Contains(preFatalProcesses[0], "terminated or no processes found") {
						logCtx.WithFields(logrus.Fields{
							"processGroupId":        pgid,
							"processGroupProcesses": preFatalProcesses,
							"capturePoint":          "pre-fatal-signal",
						}).Info("Process group info captured before fatal signal")
					}
				}
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
		// Capture process group info right when command finishes (might catch lingering processes)
		if cmd.Process != nil {
			pgid := cmd.Process.Pid
			postExitProcesses := getProcessGroupInfo(pgid)
			if len(postExitProcesses) > 0 && !strings.Contains(postExitProcesses[0], "terminated or no processes found") {
				logCtx.WithFields(logrus.Fields{
					"processGroupId":        pgid,
					"processGroupProcesses": postExitProcesses,
					"capturePoint":          "post-command-exit",
				}).Info("Process group info captured right after command exit")
			}
		}
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

// getProcessGroupInfo returns information about processes in the given process group
func getProcessGroupInfo(pgid int) []string {
	if pgid <= 0 {
		return nil
	}

	// Use ps to get process group information with more details
	psCmd := exec.Command("ps", "-o", "pid,ppid,pgid,etime,comm,args", "-g", strconv.Itoa(pgid))
	output, err := psCmd.Output()

	// ps returns exit status 1 when no processes are found in the process group
	// This is normal behavior, not an error condition
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// Exit code 1 typically means no processes found, check if we got header output
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) <= 1 {
				return []string{"Process group terminated or no processes found"}
			}
			// Continue processing the output even with exit code 1
		} else {
			// Other types of errors (command not found, permission denied, etc.)
			return []string{fmt.Sprintf("Failed to get process group info: %v", err)}
		}
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) <= 1 {
		return []string{"Process group terminated or no processes found"}
	}

	// Skip header line and format the output
	var processes []string
	for i, line := range lines {
		if i == 0 {
			continue // Skip header
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		pid, ppid, pgidStr, elapsed, comm, args, ok := parsePsLineFivePlus(line)
		if ok {
			processInfo := fmt.Sprintf("PID=%s PPID=%s PGID=%s ELAPSED=%s COMM=%s ARGS=%s",
				pid, ppid, pgidStr, elapsed, comm, args)

			// Add working directory information if available
			if workDir := getProcessWorkingDir(pid); workDir != "" {
				processInfo += fmt.Sprintf(" CWD=%s", workDir)
			}

			processes = append(processes, processInfo)
		} else {
			processes = append(processes, fmt.Sprintf("Raw: %s", line))
		}
	}

	if len(processes) == 0 {
		return []string{"Process group terminated or no processes found"}
	}

	return processes
}

// getProcessWorkingDir returns the working directory of a process
func getProcessWorkingDir(pid string) string {
	// Try to read the working directory from /proc/PID/cwd (Linux) or use lsof
	if cwd, err := os.Readlink(fmt.Sprintf("/proc/%s/cwd", pid)); err == nil {
		return cwd
	}

	// Fallback to lsof on macOS/other systems
	lsofCmd := exec.Command("lsof", "-p", pid, "-d", "cwd", "-Fn")
	if output, err := lsofCmd.Output(); err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "n") {
				return strings.TrimPrefix(line, "n")
			}
		}
	}

	return ""
}

// findProcessesInDirectory finds processes that have their working directory in or under the specified directory
func findProcessesInDirectory(processes []string, targetDir string) []string {
	var matches []string
	for _, process := range processes {
		if strings.Contains(process, fmt.Sprintf("CWD=%s", targetDir)) ||
			strings.Contains(process, fmt.Sprintf("CWD=%s/", targetDir)) {
			matches = append(matches, process)
		}
	}
	return matches
}

// parsePsLineFivePlus splits a ps output line with at least 5 whitespace-separated fields,
// returning PID, PPID, PGID, ELAPSED, COMM, and the remaining ARGS (which may contain spaces).
func parsePsLineFivePlus(line string) (string, string, string, string, string, string, bool) {
	// Extract first five fields, rest is ARGS
	fields := make([]string, 0, 6)
	start := -1
	inSpace := true
	for i, r := range line {
		if r == ' ' || r == '\t' {
			if !inSpace {
				fields = append(fields, line[start:i])
				if len(fields) == 5 {
					rest := strings.TrimLeft(line[i+1:], " \t")
					fields = append(fields, rest)
					break
				}
			}
			inSpace = true
		} else {
			if inSpace {
				start = i
			}
			inSpace = false
		}
	}
	if !inSpace && len(fields) < 5 && start >= 0 {
		fields = append(fields, line[start:])
	}
	if len(fields) < 6 { // need at least PID, PPID, PGID, ELAPSED, COMM, ARGS
		return "", "", "", "", "", "", false
	}
	return fields[0], fields[1], fields[2], fields[3], fields[4], fields[5], true
}
