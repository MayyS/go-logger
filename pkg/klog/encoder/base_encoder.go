package encoder

import (
	"fmt"

	"github.com/Mayys/log/internal/custom"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

type CustomEncoder interface {
	EncodeMsg(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error)
	EncodeFields(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error)
	EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error)
	Clone() zapcore.Encoder
}

var _ CustomEncoder = (*BaseEncoder)(nil)

type BaseEncoder struct {
	*customJsonEncoder
}

func NewBaseEncoder(cfg zapcore.EncoderConfig) *BaseEncoder {
	return &BaseEncoder{
		newCustomConconsoleEncoder(cfg),
	}
}

func (b *BaseEncoder) EncodeMsg(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	line := bufPool.Get()
	arr := custom.GetSliceEncoder()
	if b.TimeKey != "" && b.EncodeTime != nil && !ent.Time.IsZero() {
		b.EncodeTime(ent.Time, arr)
	}
	if b.LevelKey != "" && b.EncodeLevel != nil {
		b.EncodeLevel(ent.Level, arr)
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
func (b *BaseEncoder) EncodeFields(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	line := bufPool.Get()
	// Add any structured context.
	b.writeContext(line, fields)
	return line, nil
}
func (b *BaseEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	panic("should be impletement")
}

func (b *BaseEncoder) Clone() zapcore.Encoder {
	return &BaseEncoder{
		b.customJsonEncoder.Clone().(*customJsonEncoder),
	}
}

func (b *BaseEncoder) writeContext(line *buffer.Buffer, extra []zapcore.Field) {
	context := b.customJsonEncoder.Clone().(*customJsonEncoder)
	defer func() {
		// putJSONEncoder assumes the buffer is still used, but we write out the buffer so
		// we can free it.
		context.buf.Free()
		putJSONEncoder(context)
	}()

	addFields(context, extra)
	context.closeOpenNamespaces()
	if context.buf.Len() == 0 {
		return
	}

	b.AddSeparatorIfNecessary(line)
	line.AppendByte('{')
	line.Write(context.buf.Bytes())
	line.AppendByte('}')
}
