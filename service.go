package go_rpc

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

type methodType struct {
	method reflect.Method
	ArgvType reflect.Type
	ReplyType reflect.Type
	numCalls uint64
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value

	if m.ArgvType.Kind() == reflect.Ptr {
		argv = reflect.New(m.ArgvType.Elem())
	} else {
		argv = reflect.New(m.ArgvType).Elem()
	}

	return argv
}

func (m *methodType) newReplyV() reflect.Value {
	reply := reflect.New(m.ReplyType.Elem())

	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		reply.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		reply.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}

	return reply
}

type service struct {
	name string
	typ reflect.Type
	rcv reflect.Value
	methods map[string]*methodType
}

func newService(rcv interface{}) *service {
	s := new(service)
	s.rcv = reflect.ValueOf(rcv)
	s.typ = reflect.TypeOf(rcv)
	s.name = reflect.Indirect(s.rcv).Type().Name()
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not available server\n", s.name)
	}

	s.registerMethods()

	return s
}

func (s *service) registerMethods() {
	s.methods = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}

		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}

		argvType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argvType) || !isExportedOrBuiltinType(replyType) {
			continue
		}

		s.methods[method.Name] = &methodType{
			method: method,
			ArgvType: argvType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

func (s *service) call(m *methodType, argv, replyV reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.rcv, argv, replyV})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}

	return nil
}