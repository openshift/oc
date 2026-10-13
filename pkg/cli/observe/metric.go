package observe

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"k8s.io/klog/v2"
)

var (
	observeCounts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "observe_counts",
			Help: "Number of changes observed to the underlying resource.",
		},
		[]string{"type"},
	)
	execDurations = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "observe_exec_durations_milliseconds",
			Help: "Item execution latency distributions.",
		},
		[]string{"type", "exit_code"},
	)
	nameExecDurations = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "observe_name_exec_durations_milliseconds",
			Help: "Name list execution latency distributions.",
		},
		[]string{"exit_code"},
	)
)

func measureCommandDuration(m *prometheus.SummaryVec, fn func() error, labels ...string) error {
	n := time.Now()
	err := fn()
	duration := time.Now().Sub(n)
	statusCode, ok := exitCodeForCommandError(err)
	if !ok {
		statusCode = -1
	}
	m.WithLabelValues(append(labels, strconv.Itoa(statusCode))...).Observe(float64(duration / time.Millisecond))

	if errnoError(err) == syscall.ECHILD {
		// ignore wait4 syscall errno as it means
		// that the subprocess has started and ended
		// before the wait call was made.
		return nil
	}

	return err
}

func errnoError(err error) syscall.Errno {
	if se, ok := err.(*os.SyscallError); ok {
		if errno, ok := se.Err.(syscall.Errno); ok {
			return errno
		}
	}

	return 0
}

func exitCodeForCommandError(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	if exit, ok := err.(*exec.ExitError); ok {
		if ws, ok := exit.ProcessState.Sys().(syscall.WaitStatus); ok {
			return ws.ExitStatus(), true
		}
	}
	return 0, false
}

func retryCommandError(onExitStatus, times int, fn func() error) error {
	err := fn()
	if err != nil && onExitStatus != 0 && times > 0 {
		if status, ok := exitCodeForCommandError(err); ok {
			if status == onExitStatus {
				klog.V(4).Infof("retrying command: %v", err)
				return retryCommandError(onExitStatus, times-1, fn)
			}
		}
	}
	return err
}
