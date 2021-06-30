package wangRPC

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

// 通过反射实现结构体与服务的映射关系

type service struct {
	name   string           // 映射的结构体的名称
	typ    reflect.Type     // 映射的结构体的类型
	receiver   reflect.Value    // 映射的结构体的实例本身，保留receiver是因为在调用时需要receiver作为第0个参数
	method map[string]*methodType  // 存储映射的结构体的所有符合条件的方法
}

func newService(receiver interface{}) *service {
	s := new(service)
	s.receiver = reflect.ValueOf(receiver)
	s.name = reflect.Indirect(s.receiver).Type().Name()
	s.typ  = reflect.TypeOf(receiver)
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethods()
	return s
}

// 注册结构体中所有符合规范的方法
// 两个导出或内置类型的入参（反射时为3个，第0个是自身）
// 返回值有且只有1个，且类型为error
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	// 遍历方法
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		// 检查method的参数和返回值的个数是否符合规范
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		// 检查返回值的类型是否为error
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		// 获取两个参数 (第0个参数是方法接收者自身)
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}

		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// 通过反射值调用方法
func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.receiver, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}


type methodType struct {
	method    reflect.Method   // 方法本身
	ArgType   reflect.Type     // 第一个参数的类型
	ReplyType reflect.Type     // 第二个参数的类型
	numCalls  uint64           // 统计方法调用次数
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	// 参数可能是指针类型或值类型
	if m.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

func (m *methodType) newReplyv() reflect.Value {
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv
}





























