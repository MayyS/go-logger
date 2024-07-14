package encoder

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"time"
	"unicode/utf8"

	"github.com/Mayys/log/internal/pool"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// For JSON-escaping; see jsonEncoder.safeAddString below.
const _hex = "0123456789abcdef"

var _customPool = pool.New(func() *customJsonEncoder {
	return &customJsonEncoder{}
})

type customJsonEncoder struct {
	*zapcore.EncoderConfig
	buf            *buffer.Buffer
	spaced         bool // include spaces after colons and commas
	openNamespaces int

	// for encoding generic values by reflection
	reflectBuf *buffer.Buffer
	reflectEnc zapcore.ReflectedEncoder
}

// NewCustomConconsoleEncoder creates a new map-backed ObjectEncoder.
func newCustomConconsoleEncoder(cfg zapcore.EncoderConfig) *customJsonEncoder {

	if cfg.ConsoleSeparator == "" {
		// Use a default delimiter of '\t' for backwards compatibility
		cfg.ConsoleSeparator = "\t"
	}
	if cfg.SkipLineEnding {
		cfg.LineEnding = ""
	} else if cfg.LineEnding == "" {
		cfg.LineEnding = zapcore.DefaultLineEnding
	}

	// If no EncoderConfig.NewReflectedEncoder is provided by the user, then use default
	if cfg.NewReflectedEncoder == nil {
		cfg.NewReflectedEncoder = defaultReflectedEncoder
	}

	return &customJsonEncoder{
		EncoderConfig: &cfg,
		buf:           bufPool.Get(),
		spaced:        false,
	}
}

func (c *customJsonEncoder) Clone() zapcore.Encoder {
	clone := _customPool.Get()
	clone.EncoderConfig = c.EncoderConfig
	clone.spaced = c.spaced
	clone.openNamespaces = c.openNamespaces
	clone.buf = bufPool.Get()
	clone.buf.Write(c.buf.Bytes())
	return clone
}

var (
	bufPool = buffer.NewPool()
)

func (c *customJsonEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	panic("not impelement yet")
}

func (m *customJsonEncoder) writeFiles(fields []zapcore.Field) {
	for i := range fields {
		fields[i].AddTo(m)
	}
	m.closeOpenNamespaces()
}

func (m *customJsonEncoder) AddArray(key string, arr zapcore.ArrayMarshaler) error {
	m.addKey(key)
	return m.AppendArray(arr)
}

func (m *customJsonEncoder) AddObject(key string, obj zapcore.ObjectMarshaler) error {
	m.addKey(key)
	return m.AppendObject(obj)
}

func (m *customJsonEncoder) AddBinary(key string, val []byte) {
	m.AddString(key, base64.StdEncoding.EncodeToString(val))
}

func (m *customJsonEncoder) AddByteString(key string, val []byte) {
	m.addKey(key)
	m.AppendByteString(val)
}

func (m *customJsonEncoder) AddBool(key string, val bool) {
	m.addKey(key)
	m.AppendBool(val)
}

func (m *customJsonEncoder) AddComplex128(key string, val complex128) {
	m.addKey(key)
	m.AppendComplex128(val)
}

func (m *customJsonEncoder) AddComplex64(key string, val complex64) {
	m.addKey(key)
	m.AppendComplex64(val)
}

func (m *customJsonEncoder) AddDuration(key string, val time.Duration) {
	m.addKey(key)
	m.AppendDuration(val)
}

func (m *customJsonEncoder) AddFloat64(key string, val float64) {
	m.addKey(key)
	m.AppendFloat64(val)
}

func (m *customJsonEncoder) AddFloat32(key string, val float32) {
	m.addKey(key)
	m.AppendFloat32(val)
}

func (m *customJsonEncoder) AddInt64(key string, val int64) {
	m.addKey(key)
	m.AppendInt64(val)
}

var nullLiteralBytes = []byte("null")

// Only invoke the standard JSON encoder if there is actually something to
// encode; otherwise write JSON null literal directly.
func (m *customJsonEncoder) encodeReflected(obj interface{}) ([]byte, error) {
	if obj == nil {
		return nullLiteralBytes, nil
	}
	m.resetReflectBuf()
	if err := m.reflectEnc.Encode(obj); err != nil {
		return nil, err
	}
	m.reflectBuf.TrimNewline()
	return m.reflectBuf.Bytes(), nil
}

func (m *customJsonEncoder) AddReflected(key string, obj interface{}) error {
	valueBytes, err := m.encodeReflected(obj)
	if err != nil {
		return err
	}
	m.addKey(key)
	_, err = m.buf.Write(valueBytes)
	return err
}

func (m *customJsonEncoder) OpenNamespace(key string) {
	m.addKey(key)
	m.buf.AppendByte('{')
	m.openNamespaces++
}

func (m *customJsonEncoder) AddString(key, val string) {
	m.addKey(key)
	m.AppendString(val)
}

func (m *customJsonEncoder) AddTime(key string, val time.Time) {
	m.addKey(key)
	m.AppendTime(val)
}

func (m *customJsonEncoder) AddUint64(key string, val uint64) {
	m.addKey(key)
	m.AppendUint64(val)
}

func (m *customJsonEncoder) AppendArray(arr zapcore.ArrayMarshaler) error {
	m.addElementSeparator()
	m.buf.AppendByte('[')
	err := arr.MarshalLogArray(m)
	m.buf.AppendByte(']')
	return err
}

func (m *customJsonEncoder) AppendObject(obj zapcore.ObjectMarshaler) error {
	// Close ONLY new openNamespaces that are created during
	// AppendObject().
	old := m.openNamespaces
	m.openNamespaces = 0
	m.addElementSeparator()
	m.buf.AppendByte('{')
	err := obj.MarshalLogObject(m)
	m.buf.AppendByte('}')
	m.closeOpenNamespaces()
	m.openNamespaces = old
	return err
}

func (m *customJsonEncoder) AppendBool(val bool) {
	m.addElementSeparator()
	m.buf.AppendBool(val)
}

func (m *customJsonEncoder) AppendByteString(val []byte) {
	m.addElementSeparator()
	m.buf.AppendByte('"')
	m.safeAddByteString(val)
	m.buf.AppendByte('"')
}

// appendComplex appends the encoded form of the provided complex128 value.
// precision specifies the encoding precision for the real and imaginary
// components of the complex number.
func (m *customJsonEncoder) appendComplex(val complex128, precision int) {
	m.addElementSeparator()
	// Cast to a platform-independent, fixed-size type.
	r, i := float64(real(val)), float64(imag(val))
	m.buf.AppendByte('"')
	// Because we're always in a quoted string, we can use strconv without
	// special-casing NaN and +/-Inf.
	m.buf.AppendFloat(r, precision)
	// If imaginary part is less than 0, minus (-) sign is added by default
	// by AppendFloat.
	if i >= 0 {
		m.buf.AppendByte('+')
	}
	m.buf.AppendFloat(i, precision)
	m.buf.AppendByte('i')
	m.buf.AppendByte('"')
}

func (m *customJsonEncoder) AppendDuration(val time.Duration) {
	cur := m.buf.Len()
	if e := m.EncodeDuration; e != nil {
		e(val, m)
	}
	if cur == m.buf.Len() {
		// User-supplied EncodeDuration is a no-op. Fall back to nanoseconds to keep
		// JSON valid.
		m.AppendInt64(int64(val))
	}
}

func (m *customJsonEncoder) AppendInt64(val int64) {
	m.addElementSeparator()
	m.buf.AppendInt(val)
}

func (m *customJsonEncoder) AppendReflected(val interface{}) error {
	valueBytes, err := m.encodeReflected(val)
	if err != nil {
		return err
	}
	m.addElementSeparator()
	_, err = m.buf.Write(valueBytes)
	return err
}

func (m *customJsonEncoder) AppendString(val string) {
	m.addElementSeparator()
	m.buf.AppendByte('"')
	m.safeAddString(val)
	m.buf.AppendByte('"')
}

func (m *customJsonEncoder) AppendTimeLayout(time time.Time, layout string) {
	m.addElementSeparator()
	m.buf.AppendByte('"')
	m.buf.AppendTime(time, layout)
	m.buf.AppendByte('"')
}

func (m *customJsonEncoder) AppendTime(val time.Time) {
	cur := m.buf.Len()
	if e := m.EncodeTime; e != nil {
		e(val, m)
	}
	if cur == m.buf.Len() {
		// User-supplied EncodeTime is a no-op. Fall back to nanos since epoch to keep
		// output JSON valid.
		m.AppendInt64(val.UnixNano())
	}
}

func (m *customJsonEncoder) AppendUint64(val uint64) {
	m.addElementSeparator()
	m.buf.AppendUint(val)
}

func (m *customJsonEncoder) AddInt(k string, v int)         { m.AddInt64(k, int64(v)) }
func (m *customJsonEncoder) AddInt32(k string, v int32)     { m.AddInt64(k, int64(v)) }
func (m *customJsonEncoder) AddInt16(k string, v int16)     { m.AddInt64(k, int64(v)) }
func (m *customJsonEncoder) AddInt8(k string, v int8)       { m.AddInt64(k, int64(v)) }
func (m *customJsonEncoder) AddUint(k string, v uint)       { m.AddUint64(k, uint64(v)) }
func (m *customJsonEncoder) AddUint32(k string, v uint32)   { m.AddUint64(k, uint64(v)) }
func (m *customJsonEncoder) AddUint16(k string, v uint16)   { m.AddUint64(k, uint64(v)) }
func (m *customJsonEncoder) AddUint8(k string, v uint8)     { m.AddUint64(k, uint64(v)) }
func (m *customJsonEncoder) AddUintptr(k string, v uintptr) { m.AddUint64(k, uint64(v)) }
func (m *customJsonEncoder) AppendComplex64(v complex64)    { m.appendComplex(complex128(v), 32) }
func (m *customJsonEncoder) AppendComplex128(v complex128)  { m.appendComplex(complex128(v), 64) }
func (m *customJsonEncoder) AppendFloat64(v float64)        { m.appendFloat(v, 64) }
func (m *customJsonEncoder) AppendFloat32(v float32)        { m.appendFloat(float64(v), 32) }
func (m *customJsonEncoder) AppendInt(v int)                { m.AppendInt64(int64(v)) }
func (m *customJsonEncoder) AppendInt32(v int32)            { m.AppendInt64(int64(v)) }
func (m *customJsonEncoder) AppendInt16(v int16)            { m.AppendInt64(int64(v)) }
func (m *customJsonEncoder) AppendInt8(v int8)              { m.AppendInt64(int64(v)) }
func (m *customJsonEncoder) AppendUint(v uint)              { m.AppendUint64(uint64(v)) }
func (m *customJsonEncoder) AppendUint32(v uint32)          { m.AppendUint64(uint64(v)) }
func (m *customJsonEncoder) AppendUint16(v uint16)          { m.AppendUint64(uint64(v)) }
func (m *customJsonEncoder) AppendUint8(v uint8)            { m.AppendUint64(uint64(v)) }
func (m *customJsonEncoder) AppendUintptr(v uintptr)        { m.AppendUint64(uint64(v)) }

func (m *customJsonEncoder) resetReflectBuf() {
	if m.reflectBuf == nil {
		m.reflectBuf = bufPool.Get()
		m.reflectEnc = m.NewReflectedEncoder(m.reflectBuf)
	} else {
		m.reflectBuf.Reset()
	}
}

func (m *customJsonEncoder) AddSeparatorIfNecessary(line *buffer.Buffer) {
	if line.Len() > 0 {
		line.AppendString(m.ConsoleSeparator)
	}
}

func (m *customJsonEncoder) truncate() {
	m.buf.Reset()
}

func (m *customJsonEncoder) closeOpenNamespaces() {
	for i := 0; i < m.openNamespaces; i++ {
		m.buf.AppendByte('}')
	}
	m.openNamespaces = 0
}

func (m *customJsonEncoder) addKey(key string) {
	m.addElementSeparator()
	m.buf.AppendByte('"')
	m.safeAddString(key)
	m.buf.AppendByte('"')
	m.buf.AppendByte(':')
	if m.spaced {
		m.buf.AppendByte(' ')
	}
}

func (m *customJsonEncoder) addElementSeparator() {
	last := m.buf.Len() - 1
	if last < 0 {
		return
	}
	switch m.buf.Bytes()[last] {
	case '{', '[', ':', ',', ' ':
		return
	default:
		m.buf.AppendByte(',')
		if m.spaced {
			m.buf.AppendByte(' ')
		}
	}
}

func putJSONEncoder(enc *customJsonEncoder) {
	if enc.reflectBuf != nil {
		enc.reflectBuf.Free()
	}
	enc.EncoderConfig = nil
	enc.buf = nil
	enc.spaced = false
	enc.openNamespaces = 0
	enc.reflectBuf = nil
	enc.reflectEnc = nil
	_customPool.Put(enc)
}

func addFields(enc zapcore.ObjectEncoder, fields []zapcore.Field) {
	for i := range fields {
		fields[i].AddTo(enc)
	}
}

func (m *customJsonEncoder) appendFloat(val float64, bitSize int) {
	m.addElementSeparator()
	switch {
	case math.IsNaN(val):
		m.buf.AppendString(`"NaN"`)
	case math.IsInf(val, 1):
		m.buf.AppendString(`"+Inf"`)
	case math.IsInf(val, -1):
		m.buf.AppendString(`"-Inf"`)
	default:
		m.buf.AppendFloat(val, bitSize)
	}
}

// safeAddString JSON-escapes a string and appends it to the internal buffer.
// Unlike the standard library's encoder, it doesn't attempt to protect the
// user from browser vulnerabilities or JSONP-related problems.
func (m *customJsonEncoder) safeAddString(s string) {
	safeAppendStringLike(
		(*buffer.Buffer).AppendString,
		utf8.DecodeRuneInString,
		m.buf,
		s,
	)
}

// safeAddByteString is no-alloc equivalent of safeAddString(string(s)) for s []byte.
func (m *customJsonEncoder) safeAddByteString(s []byte) {
	safeAppendStringLike(
		(*buffer.Buffer).AppendBytes,
		utf8.DecodeRune,
		m.buf,
		s,
	)
}

// safeAppendStringLike is a generic implementation of safeAddString and safeAddByteString.
// It appends a string or byte slice to the buffer, escaping all special characters.
func safeAppendStringLike[S []byte | string](
	// appendTo appends this string-like object to the buffer.
	appendTo func(*buffer.Buffer, S),
	// decodeRune decodes the next rune from the string-like object
	// and returns its value and width in bytes.
	decodeRune func(S) (rune, int),
	buf *buffer.Buffer,
	s S,
) {
	// The encoding logic below works by skipping over characters
	// that can be safely copied as-is,
	// until a character is found that needs special handling.
	// At that point, we copy everything we've seen so far,
	// and then handle that special character.
	//
	// last is the index of the last byte that was copied to the buffer.
	last := 0
	for i := 0; i < len(s); {
		if s[i] >= utf8.RuneSelf {
			// Character >= RuneSelf may be part of a multi-byte rune.
			// They need to be decoded before we can decide how to handle them.
			r, size := decodeRune(s[i:])
			if r != utf8.RuneError || size != 1 {
				// No special handling required.
				// Skip over this rune and continue.
				i += size
				continue
			}

			// Invalid UTF-8 sequence.
			// Replace it with the Unicode replacement character.
			appendTo(buf, s[last:i])
			buf.AppendString(`\ufffd`)

			i++
			last = i
		} else {
			// Character < RuneSelf is a single-byte UTF-8 rune.
			if s[i] >= 0x20 && s[i] != '\\' && s[i] != '"' {
				// No escaping necessary.
				// Skip over this character and continue.
				i++
				continue
			}

			// This character needs to be escaped.
			appendTo(buf, s[last:i])
			switch s[i] {
			case '\\', '"':
				buf.AppendByte('\\')
				buf.AppendByte(s[i])
			case '\n':
				buf.AppendByte('\\')
				buf.AppendByte('n')
			case '\r':
				buf.AppendByte('\\')
				buf.AppendByte('r')
			case '\t':
				buf.AppendByte('\\')
				buf.AppendByte('t')
			default:
				// Encode bytes < 0x20, except for the escape sequences above.
				buf.AppendString(`\u00`)
				buf.AppendByte(_hex[s[i]>>4])
				buf.AppendByte(_hex[s[i]&0xF])
			}

			i++
			last = i
		}
	}

	// add remaining
	appendTo(buf, s[last:])
}
func defaultReflectedEncoder(w io.Writer) zapcore.ReflectedEncoder {
	enc := json.NewEncoder(w)
	// For consistency with our custom JSON encoder.
	enc.SetEscapeHTML(false)
	return enc
}
