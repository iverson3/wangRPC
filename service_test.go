package wangRPC

import (
	"fmt"
	"reflect"
	"testing"
)

type Foo int

type Args struct {
	Num1 int
	Num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func (f Foo) sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func _assert(condition bool, msg string, v ...interface{})  {
	if !condition {
		panic(fmt.Sprintf("assertion failed: " + msg, v...))
	}
}

func TestNewService(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	_assert(len(s.method) == 1, "wrong service Method, expect 1, but got %d", len(s.method))

	mType := s.method["Sum"]
	_assert(mType != nil, "wrong Method, Sum shouldn't be nil")
}

func TestMethodType_Call(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	mType := s.method["Sum"]

	argv := mType.newArgv()
	replyv := mType.newReplyv()

	// 定义参数和预期的返回值
	mArgv := Args{
		Num1: 1,
		Num2: 3,
	}
	expectReply := 4

	// 设置参数
	argv.Set(reflect.ValueOf(mArgv))
	// 调用函数
	err := s.call(mType, argv, replyv)

	condition := err == nil && *replyv.Interface().(*int) == expectReply && mType.NumCalls() == 1
	_assert(condition, "failed to call Foo.Sum")

}





























