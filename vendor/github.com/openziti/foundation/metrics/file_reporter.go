package metrics

import (
	"fmt"
	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/foundation/metrics/metrics_pb"
	"github.com/pkg/errors"
	"os"
	"strings"
)

type ouputFileReporter struct {
	path        string
	maxsize     int
	metricsChan chan *metrics_pb.MetricsMessage
	formatter   Formatter
}

func (reporter *ouputFileReporter) AcceptMetrics(message *metrics_pb.MetricsMessage) {
	reporter.metricsChan <- message
}

// Message handler that write node and link metrics to a file in json format
func NewOutputFileMetricsHandler(cfg *outputfileConfig) (Handler, error) {
	rep := &ouputFileReporter{
		path:        cfg.path,
		maxsize:     cfg.maxsizemb,
		metricsChan: make(chan *metrics_pb.MetricsMessage, 10),
		formatter:   cfg.formatter,
	}

	go rep.run()
	return rep, nil
}

func (reporter *ouputFileReporter) run() {
	logger := pfxlog.Logger()
	logger.Info("JSON File Metrics Reporter started")
	defer logger.Warn("exited")

	for {
		select {
		case msg := <-reporter.metricsChan:
			reporter.send(msg)
		}
	}
}

func (reporter *ouputFileReporter) send(msg *metrics_pb.MetricsMessage) {
	// Check for max file size, truncate if larger than threshold
	if stat, err := os.Stat(reporter.path); err == nil {
		// get the size
		size := stat.Size()
		if size >= int64(reporter.maxsize*1024*1024) {
			if err := os.Truncate(reporter.path, 0); err != nil {
				pfxlog.Logger().WithError(err).Errorf("failure while trucating metrics log file %v to size %vM", reporter.path, reporter.maxsize)
			}
		}
	} else {
		pfxlog.Logger().WithError(err).Errorf("failure while statting metrics log file %v", reporter.path)
	}

	f, err := os.OpenFile(reporter.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0664)
	if err != nil {
		pfxlog.Logger().WithError(err).Errorf("failure while opening metrics log file %v", reporter.path)
		return
	}
	defer func() { _ = f.Close() }()

	if err = reporter.formatter.WriteTo(msg, f); err != nil {
		pfxlog.Logger().WithError(err).Errorf("failure while recording metrics event to %v", reporter.path)
	}
}

type outputfileConfig struct {
	path      string
	maxsizemb int
	formatter Formatter
}

func LoadOutputFileConfig(src map[interface{}]interface{}) (*outputfileConfig, error) {
	cfg := &outputfileConfig{
		formatter: &JsonFormatter{},
	}

	if value, found := src["path"]; found {
		if path, ok := value.(string); ok {
			// check path writablility
			cfg.path = path

			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0664)
			if err != nil {
				return nil, fmt.Errorf("cannot write to log file path: %s", path)
			} else {
				_ = f.Close()
			}
		} else {
			return nil, errors.New("invalid jsonFileReporter 'path' value")
		}
	} else {
		return nil, errors.New("missing required 'path' config for JSONFileReporter")
	}

	if value, found := src["maxsizemb"]; found {
		if maxsizemb, ok := value.(int); ok {
			cfg.maxsizemb = maxsizemb
		} else {
			// just set a default
			return nil, errors.New("invalid 'maxsizemb' config for JSONFileReporter")
		}
	} else {
		return nil, errors.New("missing jsonFileReporter 'maxsizemb' config")
	}

	if value, found := src["format"]; found {
		if format, ok := value.(string); ok {
			if strings.EqualFold("json", format) {
				cfg.formatter = &JsonFormatter{}
			} else if strings.EqualFold("plain", format) {
				cfg.formatter = &PlainTextFormatter{}
			} else {
				return nil, errors.Errorf("invalid 'format' for metrics output file: %v", format)
			}
		} else {
			return nil, errors.New("invalid 'format' for metrics output file")
		}
	}

	return cfg, nil
}
