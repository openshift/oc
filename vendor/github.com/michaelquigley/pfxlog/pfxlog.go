package pfxlog

import (
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh/terminal"
	"os"
)

func init() {
	// cover cases where ContextLogger is used in a package init function.
	globalOptions = &Options{StandardLogger: logrus.StandardLogger()}
}

func GlobalInit(level logrus.Level, options *Options) {
	if defaultEnv("PFXLOG_NO_JSON", false) || terminal.IsTerminal(int(os.Stdout.Fd())) {
		logrus.SetFormatter(NewFormatter(options))
	} else {
		logrus.SetFormatter(&logrus.JSONFormatter{TimestampFormat: options.JsonTimestampFormat})
	}
	logrus.SetLevel(level)
	logrus.SetReportCaller(true)

	globalOptions = options

	for _, logLevel := range logrus.AllLevels {
		logger := CloneLogger(options.StandardLogger)
		logger.Level = logLevel
		if _, found := globalOptions.Loggers[logLevel]; !found {
			globalOptions.Loggers[logLevel] = logger
		}
	}
}

func GlobalConfig(f func(*Options) *Options) {
	globalOptions = f(globalOptions)
}

func Logger() *Builder {
	return &Builder{logrus.NewEntry(globalOptions.StandardLogger)}
}

func ContextLogger(context string) *Builder {
	return &Builder{globalOptions.StandardLogger.WithField("context", context)}
}

type Builder struct {
	*logrus.Entry
}

type Wirer interface {
	WireEntry(entry *logrus.Entry) *logrus.Entry
}

type EntryWireF func(entry *logrus.Entry) *logrus.Entry

func (self EntryWireF) WireEntry(entry *logrus.Entry) *logrus.Entry {
	return self(entry)
}

func (self *Builder) Wire(wirer Wirer) *Builder {
	self.Entry = wirer.WireEntry(self.Entry)
	return self
}

func (self *Builder) Data(data interface{}) *Builder {
	if globalOptions.DataFielder != nil {
		self.Entry = globalOptions.DataFielder(data, self.Entry)
	}
	return self
}

func (self *Builder) Enabled(data interface{}) *Builder {
	if globalOptions.EnabledChecker != nil && !globalOptions.EnabledChecker(data) {
		self.Entry.Logger = globalOptions.Loggers[logrus.PanicLevel]
	}
	return self
}

func (self *Builder) Channels(channels ...string) *Builder {
	for _, channel := range channels {
		if _, found := globalOptions.ActiveChannels[channel]; found {
			self.Entry = self.Entry.WithField("channels", channels)
			return self
		}
	}
	self.Entry.Logger = globalOptions.Loggers[logrus.PanicLevel]
	return self
}

func (self *Builder) WithChannels(channels ...string) *Builder {
	for _, channel := range channels {
		if level, found := globalOptions.ChannelLogLevelOverrides[channel]; found {
			if level > self.Entry.Logger.Level {
				self.Entry.Logger = globalOptions.Loggers[level]
			}
		}
	}
	self.Entry = self.Entry.WithField("channels", channels)
	return self
}

func ChannelLogger(channels ...string) *Builder {
	return Logger().WithChannels(channels...)
}

func LevelLogger(level logrus.Level) *logrus.Logger {
	return globalOptions.Loggers[level]
}

func SetFormatter(f logrus.Formatter) {
	globalOptions.StandardLogger.SetFormatter(f)
	for _, logger := range globalOptions.Loggers {
		logger.SetFormatter(f)
	}
}

var globalOptions *Options
