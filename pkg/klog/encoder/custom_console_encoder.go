package encoder

import (
	"fmt"

	"github.com/Mayys/log/internal/custom"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

type CustomConsoleEncoder struct {
	*BaseEncoder
	traceTag string
	traceId  string
}

func NewCustomConsoleEncoder(cfg zapcore.EncoderConfig, traceTag string) *CustomConsoleEncoder {
	return &CustomConsoleEncoder{
		NewBaseEncoder(cfg),
		traceTag,
		"",
	}
}

func (m *CustomConsoleEncoder) AddString(key, val string) {
	if key == m.traceTag {
		m.traceId = val
	} else {
		m.customJsonEncoder.AddString(key, val)
	}
}

func (b *CustomConsoleEncoder) EncodeMsg(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	line := bufPool.Get()
	arr := custom.GetSliceEncoder()
	if b.TimeKey != "" && b.EncodeTime != nil && !ent.Time.IsZero() {
		b.EncodeTime(ent.Time, arr)
	}
	if b.LevelKey != "" && b.EncodeLevel != nil {
		b.EncodeLevel(ent.Level, arr)
	}
	if b.traceId != "" {
		arr.AppendString(fmt.Sprintf("[%s:%s]", b.traceTag, b.traceId))
	}
	if ent.LoggerName != "" && b.NameKey != "" {
		nameEncoder := b.EncodeName

		if nameEncoder == nil {
			// Fall back to FullNameEncoder for backward compatibility.
			nameEncoder = zapcore.FullNameEncoder
		}

		nameEncoder(ent.LoggerName, arr)
	}
	if ent.Caller.Defined {
		if b.CallerKey != "" && b.EncodeCaller != nil {
			b.EncodeCaller(ent.Caller, arr)
		}
		if b.FunctionKey != "" {
			arr.AppendString(ent.Caller.Function)
		}
	}
	for i := range arr.Elems {
		if i > 0 {
			line.AppendString(b.ConsoleSeparator)
		}
		fmt.Fprint(line, arr.Elems[i])
	}
	custom.PutSliceEncoder(arr)

	// Add the message itself.
	if b.MessageKey != "" {
		b.AddSeparatorIfNecessary(line)
		line.AppendString(ent.Message)
	}
	return line, nil
}

func (b *CustomConsoleEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	line, err := b.EncodeMsg(ent, fields)
	if err != nil {
		return nil, err
	}
	line2, err := b.EncodeFields(ent, fields)
	if err != nil {
		return nil, err
	}
	line.AppendBytes(line2.Bytes())
	// If there's no stacktrace key, honor that; this allows users to force
	// single-line output.
	if ent.Stack != "" && b.StacktraceKey != "" {
		line.AppendByte('\n')
		line.AppendString(ent.Stack)
	}

	line.AppendString(b.LineEnding)
	return line, nil
}

func (b *CustomConsoleEncoder) Clone() zapcore.Encoder {
	return &CustomConsoleEncoder{
		b.BaseEncoder.Clone().(*BaseEncoder),
		b.traceTag,
		""}
}
