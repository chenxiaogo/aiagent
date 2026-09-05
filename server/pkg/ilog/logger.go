package ilog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"aiagent/pkg/tracex"
)

var global *zap.SugaredLogger

// OptionFunc 日志配置函数。
type OptionFunc func(*options)

type options struct {
	appName        string
	logLevel       string
	writeToConsole bool
	writeToFile    bool
	logPath        string
}

// WithAppName 设置应用名。
func WithAppName(name string) OptionFunc {
	return func(o *options) { o.appName = name }
}

// WithLogLevel 设置日志级别。
func WithLogLevel(level string) OptionFunc {
	return func(o *options) { o.logLevel = level }
}

// WithWriteToConsole 是否输出到控制台。
func WithWriteToConsole(b bool) OptionFunc {
	return func(o *options) { o.writeToConsole = b }
}

// WithWriteToFile 是否输出到文件。
func WithWriteToFile(b bool) OptionFunc {
	return func(o *options) { o.writeToFile = b }
}

// WithLogPath 设置日志文件路径。
func WithLogPath(p string) OptionFunc {
	return func(o *options) { o.logPath = p }
}

// NewLogger 初始化全局日志。
func NewLogger(opts ...OptionFunc) error {
	o := &options{
		appName:        "aiagent",
		logLevel:       "info",
		writeToConsole: true,
	}
	for _, fn := range opts {
		fn(o)
	}

	level := parseLevel(o.logLevel)
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	var cores []zapcore.Core
	if o.writeToConsole {
		cores = append(cores, zapcore.NewCore(
			zapcore.NewConsoleEncoder(encoderConfig),
			zapcore.AddSync(os.Stdout),
			level,
		))
	}
	if o.writeToFile && o.logPath != "" {
		os.MkdirAll(o.logPath, 0755)
		filename := filepath.Join(o.logPath, fmt.Sprintf("%s.log", o.appName))
		hook := &lumberjack.Logger{
			Filename:   filename,
			MaxSize:    100,
			MaxBackups: 10,
			MaxAge:     30,
			Compress:   true,
		}
		fileEncoderConfig := encoderConfig
		fileEncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		cores = append(cores, zapcore.NewCore(
			zapcore.NewJSONEncoder(fileEncoderConfig),
			zapcore.AddSync(hook),
			level,
		))
	}

	core := zapcore.NewTee(cores...)
	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	global = logger.Sugar()
	return nil
}

func parseLevel(s string) zapcore.Level {
	switch s {
	case "debug":
		return zapcore.DebugLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// GetLogger 返回全局日志实例。
func GetLogger() *zap.SugaredLogger { return global }

// Sync 刷新日志缓冲。
func Sync() {
	if global != nil {
		_ = global.Sync()
	}
}

// Trace 从 context 提取链路信息并返回带 trace/span 字段的日志实例。
func Trace(ctx context.Context) Logger {
	tid := tracex.TraceIDFromContext(ctx)
	sid := tracex.SpanIDFromContext(ctx)
	s := global
	if tid != "" {
		s = s.With("traceId", tid)
	}
	if sid != "" {
		s = s.With("spanId", sid)
	}
	return &sugarLogger{s}
}

// FromGin 从 gin.Context 提取链路日志。
// 等价于 Trace(tracex.FromRequest(c))，供 Handler 打带链路 ID 的业务日志。
func FromGin(c *gin.Context) Logger {
	if c == nil {
		return Trace(context.Background())
	}
	return Trace(tracex.FromRequest(c))
}

// Logger 日志接口。
type Logger interface {
	Debug(args ...interface{})
	Debugf(template string, args ...interface{})
	Info(args ...interface{})
	Infof(template string, args ...interface{})
	Warn(args ...interface{})
	Warnf(template string, args ...interface{})
	Error(args ...interface{})
	Errorf(template string, args ...interface{})
}

type sugarLogger struct {
	*zap.SugaredLogger
}

// 全局便捷函数

func Debug(args ...interface{}) {
	global.Debug(args...)
}

func Debugf(template string, args ...interface{}) {
	global.Debugf(template, args...)
}

func Info(args ...interface{}) {
	global.Info(args...)
}

func Infof(template string, args ...interface{}) {
	global.Infof(template, args...)
}

func Warn(args ...interface{}) {
	global.Warn(args...)
}

func Warnf(template string, args ...interface{}) {
	global.Warnf(template, args...)
}

func Error(args ...interface{}) {
	global.Error(args...)
}

func Errorf(template string, args ...interface{}) {
	global.Errorf(template, args...)
}

func init() {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime: func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
		},
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		zapcore.DebugLevel,
	)
	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	global = logger.Sugar()
}