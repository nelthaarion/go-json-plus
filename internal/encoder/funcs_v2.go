package encoder

import (
	"encoding/binary"
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

// The functions of marshaling of WithMarshalers of encoding/json/v2: a value of a type which a function takes is
// written by the function, before its method or its kind. The opcodes of a type depend on the shape of the
// functions, so they are compiled for the shape and cached by it, apart from the tables shared by every goroutine.

// MarshalFuncKind is the kind of a function of marshaling. It is part of the key of the cached opcodes, because
// the two kinds write their values differently.
type MarshalFuncKind uint8

const (
	// MarshalFuncKindMarshal is a function which returns the bytes of its value.
	MarshalFuncKindMarshal MarshalFuncKind = iota + 1
	// MarshalFuncKindMarshalTo is a function which writes its value to an encoder.
	MarshalFuncKindMarshalTo
)

// MarshalFunc is a function of marshaling, for the values of the types castable to Type ( see runtime.CastableTo ).
type MarshalFunc struct {
	Type reflect.Type
	Kind MarshalFuncKind
	// Append appends the value at p, of the type from: ErrUseDefault if the function declined to write it, which
	// lets the next function, the method or the kind of the type write it.
	Append func(ctx *RuntimeContext, b []byte, p unsafe.Pointer, from reflect.Type) ([]byte, error)
}

// MarshalFuncs are the functions of marshaling of a call, in the order they are tried, with the opcodes compiled
// for them.
type MarshalFuncs struct {
	Funcs []MarshalFunc
	// shape identifies the shape of Funcs. The opcodes are cached by it, so a Marshalers value built for every call
	// does not compile the opcodes again. It contains no function or closure.
	shape *marshalFuncShape
}

// marshalFuncShape is the cached shape of a list of functions: its argument types, kinds, and order. The opcodes
// refer to the functions by their indexes and call the ones of the current call, so a shape keeps no function or
// closure.
type marshalFuncShape struct {
	codeSets sync.Map // map[modeKey]*OpcodeSet
	defaults sync.Map // map[defaultKey]*OpcodeSet
	castable sync.Map // map[reflect.Type][]int: the indexes of the functions which take a value of the type
}

// NewMarshalFuncs returns the functions of marshaling of a call, with the cache of their opcodes.
func NewMarshalFuncs(funcs []MarshalFunc) *MarshalFuncs {
	signature := marshalFuncsSignature(funcs)
	shape := newMarshalFuncShape(signature)
	return &MarshalFuncs{Funcs: funcs, shape: shape}
}

// marshalFuncsSignature returns the identity of the shape of funcs: their argument types, their kinds, and their
// order. The address of a type is its identity in a program. The signature has no function or closure in it.
func marshalFuncsSignature(funcs []MarshalFunc) string {
	if len(funcs) == 0 {
		return ""
	}
	buf := make([]byte, len(funcs)*9)
	var ptr [8]byte
	for i := range funcs {
		binary.LittleEndian.PutUint64(ptr[:], uint64(uintptr(runtime.TypePtr(funcs[i].Type))))
		copy(buf[i*9:], ptr[:])
		buf[i*9+8] = byte(funcs[i].Kind)
	}
	return string(buf)
}

// maxMarshalFuncShapes is the maximum number of shapes of functions which are kept globally. A program can create
// an unbounded number of lists of functions. The shapes are small, but keeping all of them would let a program grow
// the cache without limit. A shape past the limit is not kept, and its opcodes are compiled for every call as they
// were before the cache existed.
const maxMarshalFuncShapes = 1024

// marshalFuncShapes are the shapes of the functions which are kept globally.
var marshalFuncShapes sync.Map // map[string]*marshalFuncShape

// marshalFuncShapeCount is the number of the shapes kept globally.
var marshalFuncShapeCount atomic.Int64

// newMarshalFuncShape returns the shape of the functions of the signature, keeping it globally if the number of the
// shapes is below the limit.
func newMarshalFuncShape(signature string) *marshalFuncShape {
	if v, ok := marshalFuncShapes.Load(signature); ok {
		return v.(*marshalFuncShape)
	}
	shape := &marshalFuncShape{}
	if marshalFuncShapeCount.Add(1) > maxMarshalFuncShapes {
		marshalFuncShapeCount.Add(-1)
		return shape
	}
	v, loaded := marshalFuncShapes.LoadOrStore(signature, shape)
	if loaded {
		marshalFuncShapeCount.Add(-1)
		return v.(*marshalFuncShape)
	}
	return shape
}

// codeSet returns the opcodes of the type for the functions, compiling them if the type is new.
func (f *MarshalFuncs) codeSet(typeptr uintptr, mode compileMode) (*OpcodeSet, error) {
	key := modeKey{typeptr: typeptr, mode: mode}
	if v, ok := f.shape.codeSets.Load(key); ok {
		return v.(*OpcodeSet), nil
	}
	c := newCompiler(mode)
	c.funcs = f
	codeSet, err := c.compile(typeptr)
	if err != nil {
		return nil, err
	}
	v, _ := f.shape.codeSets.LoadOrStore(key, codeSet)
	return v.(*OpcodeSet), nil
}

// funcsOf returns the indexes of the functions which take a value of the type, in their order.
func (f *MarshalFuncs) funcsOf(typ reflect.Type) []int {
	if f == nil {
		return nil
	}
	if v, ok := f.shape.castable.Load(typ); ok {
		return v.([]int)
	}
	var funcs []int
	for i := range f.Funcs {
		if runtime.CastableTo(typ, f.Funcs[i].Type) {
			funcs = append(funcs, i)
		}
	}
	v, _ := f.shape.castable.LoadOrStore(typ, funcs)
	return v.([]int)
}

// appendByFuncs appends the value at p, of typ, by the first of the functions which doesn't decline it, or
// returns ErrUseDefault if they all do. The indexes refer to the functions of the current call.
func appendByFuncs(ctx *RuntimeContext, b []byte, p unsafe.Pointer, typ reflect.Type, funcs []int) ([]byte, error) {
	for _, i := range funcs {
		out, err := ctx.Option.Funcs.Funcs[i].Append(ctx, b, p, typ)
		if err != ErrUseDefault {
			return out, err
		}
	}
	return b, ErrUseDefault
}

// v2FuncsCode returns the code of a value of typ, addressable or not, written by the functions of marshaling, and
// true, if a function takes it. A value which they all decline is written by its default representation ( see
// appendDefault ).
func (c *Compiler) v2FuncsCode(typ reflect.Type, addressable bool) (Code, bool) {
	funcs := c.funcs.funcsOf(typ)
	if len(funcs) == 0 {
		return nil, false
	}
	return c.appendFuncCode(typ, func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
		out, err := appendByFuncs(ctx, b, p, typ, funcs)
		if err == ErrUseDefault {
			return appendDefault(ctx, b, typ, p, noMethod, addressable)
		}
		return out, err
	}), true
}
